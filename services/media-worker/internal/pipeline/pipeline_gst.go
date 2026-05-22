//go:build cgo

// MCU pipeline на GStreamer + go-gst.
//
// Архитектура (один pipeline на комнату):
//
//   per peer (динамически):
//     appsrc(rtp video) -> rtpjitterbuffer -> rtpvp8depay -> vp8dec -> videoconvert -> videoscale -> caps -> queue
//                                                                                                             \
//                                                                                                              ─> compositor (vmix).sink_N
//     appsrc(rtp audio) -> rtpjitterbuffer -> rtpopusdepay -> opusdec -> audioconvert -> audioresample -> queue
//                                                                                                             \
//                                                                                                              ─> audiomixer (amix).sink_N
//
//   static (создаётся один раз):
//     vmix -> caps(I420,WxH,FPS) -> tee name=vtee
//        vtee. ! queue ! <video encoder> ! rtpXpay ! appsink name=video-sink
//        vtee. ! queue ! valve name=recv ! videoconvert ! x264enc ! mp4mux name=mp4 ! filesink location=...
//     amix -> caps(S16LE,48000,stereo) -> tee name=atee
//        atee. ! queue ! opusenc ! rtpopuspay ! appsink name=audio-sink
//        atee. ! queue ! valve name=reca ! audioconvert ! voaacenc ! mp4.
//
// На вход в appsrc приходят raw RTP-пакеты (включая RTP-header) из room.go.
// Из appsink получаются уже re-payloaded RTP-пакеты, готовые к пушу в Pion TrackLocalStaticRTP.
package pipeline

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-gst/go-glib/glib"
	"github.com/go-gst/go-gst/gst"
	"github.com/go-gst/go-gst/gst/app"

	"github.com/vks4/vks4/services/media-worker/internal/layout"
)

var initOnce sync.Once

func NewGstFactory() Factory {
	return func(spec Spec) (Pipeline, error) {
		initOnce.Do(func() {
			gst.Init(nil)
			// MainLoop нужен для GLib-сигналов; bus errors будем читать через Poll,
			// чтобы не зависеть от того, действительно ли этот loop запустился.
			loop := glib.NewMainLoop(nil, false)
			go loop.Run()
		})
		return newGstPipeline(spec)
	}
}

type gstPipeline struct {
	spec     Spec
	pipeline *gst.Pipeline

	vmix  *gst.Element
	amix  *gst.Element
	vsink *app.Sink
	asink *app.Sink

	// placeholderPads[N] — compositor pad для placeholder-источника slot-N.
	// Используется чтобы "No user" заглушка занимала свой фрейм даже если peer не подключён.
	// Peer-pad занимает то же место с zorder=10 (поверх placeholder zorder=5) — placeholder
	// автоматически скрывается, когда реальный peer есть.
	// 20 штук соответствуют 20 audio slots.
	placeholderPads [20]*gst.Pad

	// recording valves & filesink
	recValveV *gst.Element // "recv"
	recValveA *gst.Element // "reca"
	fsink     *gst.Element // "fsink"

	mu     sync.Mutex
	peers  map[string]*peerBranch
	closed bool

	videoOut chan Sample
	audioOut chan Sample
	asdCh    chan ASDLevel

	recRunning bool
	recPath    string
}

type peerBranch struct {
	id       string
	videoBin *gst.Bin
	audioBin *gst.Bin
	videoSrc *app.Source
	audioSrc *app.Source
	vMixPad  *gst.Pad // compositor.sink_N
	aMixPad  *gst.Pad // audiomixer.sink_N
	teeBin   *gst.Bin // vad tee + appsink
	videoCh  chan []byte
	audioCh  chan []byte
	closeCh  chan struct{}
}

// ----- factory ------------------------------------------------------------

func newGstPipeline(spec Spec) (*gstPipeline, error) {
	if spec.Width == 0 {
		spec.Width = 1280
	}
	if spec.Height == 0 {
		spec.Height = 720
	}
	if spec.FPS == 0 {
		spec.FPS = 30
	}
	if spec.VideoBitrate == 0 {
		spec.VideoBitrate = 800_000
	}
	if spec.AudioBitrate == 0 {
		spec.AudioBitrate = 64_000
	}

	desc := buildPipelineDesc(spec)
	pl, err := gst.NewPipelineFromString(desc)
	if err != nil {
		return nil, fmt.Errorf("parse pipeline: %w", err)
	}

	g := &gstPipeline{
		spec:     spec,
		pipeline: pl,
		peers:    map[string]*peerBranch{},
		videoOut: make(chan Sample, 32),
		audioOut: make(chan Sample, 64),
		asdCh:    make(chan ASDLevel, 128),
	}

	if g.vmix, err = pl.GetElementByName("vmix"); err != nil || g.vmix == nil {
		return nil, fmt.Errorf("vmix not found: %v", err)
	}
	if g.amix, err = pl.GetElementByName("amix"); err != nil || g.amix == nil {
		return nil, fmt.Errorf("amix not found: %v", err)
	}
	// Захватываем placeholder pads. sink_0 — фон (testsrc black), sink_1..sink_20 — placeholders
	// (по одному на каждый из 20 audio slots, чтобы можно было показать "No user" в любом фрейме).
	// По умолчанию рисуем за пределами canvas (xpos=-9999) пока applyLayout не расставит cells.
	for i := 0; i < 20; i++ {
		padName := fmt.Sprintf("sink_%d", i+1)
		pad := g.vmix.GetStaticPad(padName)
		if pad == nil {
			fmt.Fprintf(os.Stderr, "placeholder pad %s not found\n", padName)
			continue
		}
		g.placeholderPads[i] = pad
		_ = pad.SetProperty("xpos", int(-9999))
		_ = pad.SetProperty("ypos", int(-9999))
		_ = pad.SetProperty("width", int(1))
		_ = pad.SetProperty("height", int(1))
		_ = pad.SetProperty("zorder", uint(5))
	}
	g.recValveV, _ = pl.GetElementByName("recv")
	g.recValveA, _ = pl.GetElementByName("reca")
	g.fsink, _ = pl.GetElementByName("fsink")

	vsinkEl, err := pl.GetElementByName("video-sink")
	if err != nil || vsinkEl == nil {
		return nil, fmt.Errorf("video-sink not found: %v", err)
	}
	asinkEl, err := pl.GetElementByName("audio-sink")
	if err != nil || asinkEl == nil {
		return nil, fmt.Errorf("audio-sink not found: %v", err)
	}
	g.vsink = app.SinkFromElement(vsinkEl)
	g.asink = app.SinkFromElement(asinkEl)

	g.vsink.SetCallbacks(&app.SinkCallbacks{NewSampleFunc: g.onVideoSample})
	g.asink.SetCallbacks(&app.SinkCallbacks{NewSampleFunc: g.onAudioSample})

	// SyncHandler перехватывает сообщения СИНХРОННО в момент post() до того как
	// bus решает кому их отдать. Это надёжнее AddWatch для ELEMENT-сообщений от level,
	// которые могут не дойти до main-loop watch'а.
	bus := pl.GetPipelineBus()
	bus.SetSyncHandler(func(msg *gst.Message) gst.BusSyncReply {
		if msg.Type() == gst.MessageElement {
			g.handleLevelMessage(msg)
		}
		return gst.BusPass
	})
	// AddWatch — для остальных сообщений (errors, EOS) через main loop.
	bus.AddWatch(func(msg *gst.Message) bool {
		switch msg.Type() {
		case gst.MessageError:
			gerr := msg.ParseError()
			fmt.Fprintf(os.Stderr, "gst ERROR src=%s | error=%q | code=%d | debug=%q\n",
				msg.Source(), gerr.Error(), gerr.Code(), gerr.DebugString())
		case gst.MessageWarning:
			gerr := msg.ParseWarning()
			fmt.Fprintf(os.Stderr, "gst WARN src=%s | error=%q | code=%d | debug=%q\n",
				msg.Source(), gerr.Error(), gerr.Code(), gerr.DebugString())
		case gst.MessageEOS:
			fmt.Fprintf(os.Stderr, "gst EOS (src=%s)\n", msg.Source())
		case gst.MessageElement:
			g.handleLevelMessage(msg)
		default:
			if strings.HasPrefix(msg.Source(), "level-") {
				fmt.Fprintf(os.Stderr, "level msg type=%d (%s) src=%s\n",
					msg.Type(), msg.TypeName(), msg.Source())
			}
		}
		return true // keep watching
	})
	return g, nil
}

// ----- Pipeline interface -------------------------------------------------

func (p *gstPipeline) Start(ctx context.Context) error {
	if err := p.pipeline.SetState(gst.StatePlaying); err != nil {
		return fmt.Errorf("set playing: %w", err)
	}
	// SetState может вернуть STATE_CHANGE_ASYNC. Дождёмся реального перехода (макс 5с).
	go func() {
		for i := 0; i < 10; i++ {
			time.Sleep(500 * time.Millisecond)
			st := p.pipeline.GetCurrentState()
			fmt.Fprintf(os.Stderr, "pipeline state poll[%d]: %s\n", i, st)
			if st == gst.StatePlaying {
				return
			}
		}
	}()
	return nil
}

func (p *gstPipeline) Stop(ctx context.Context) error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil
	}
	p.closed = true
	branches := make([]*peerBranch, 0, len(p.peers))
	for _, b := range p.peers {
		branches = append(branches, b)
	}
	p.peers = map[string]*peerBranch{}
	p.mu.Unlock()
	for _, b := range branches {
		p.disposePeer(b)
	}
	return p.pipeline.SetState(gst.StateNull)
}

func (p *gstPipeline) AddPeer(peerID string) (chan<- []byte, chan<- []byte, error) {
	p.mu.Lock()
	if _, ok := p.peers[peerID]; ok {
		p.mu.Unlock()
		return nil, nil, fmt.Errorf("peer %s already exists", peerID)
	}
	p.mu.Unlock()

	// --- video branch ---
	// avdec_vp8 (libav) терпимее к damaged VP8 чем vp8dec (libvpx) при потерях.
	// Большой jitterbuffer latency + queue после depay снижают error rate.
	scaledW, scaledH := p.spec.Width/2, p.spec.Height/2
	// КЛЮЧЕВОЕ: явно фиксируем format=I420 — иначе compositor.sink_%u не negotiate
	// и весь bin падает в streaming stopped, reason not-negotiated (-4).
	vDesc := fmt.Sprintf(
		// videorate перед compositor даёт стабильный 30fps на peer-pad (compositor ожидает
		// regular cadence для force-live aggregation). avdec_vp8 может выдавать frames неравномерно,
		// без videorate compositor считает pad inactive и рисует только фон.
		`appsrc name=vsrc-%[1]s is-live=true format=time do-timestamp=true caps="application/x-rtp,media=video,encoding-name=VP8,clock-rate=90000,payload=96" `+
			`! rtpvp8depay `+
			`! queue max-size-buffers=32 leaky=downstream `+
			`! avdec_vp8 `+
			`! videoconvert `+
			`! videoscale add-borders=true `+
			`! videorate `+
			`! video/x-raw,format=I420,width=%[2]d,height=%[3]d,framerate=30/1,pixel-aspect-ratio=1/1 `+
			`! queue max-size-buffers=4 leaky=downstream`,
		peerID, scaledW, scaledH)

	vbin, err := gst.NewBinFromString(vDesc, true)
	if err != nil {
		return nil, nil, fmt.Errorf("video bin parse: %w", err)
	}
	if err := p.pipeline.Add(vbin.Element); err != nil {
		return nil, nil, fmt.Errorf("add video bin: %w", err)
	}
	vmixSink := p.vmix.GetRequestPad("sink_%u")
	if vmixSink == nil {
		_ = p.pipeline.Remove(vbin.Element)
		return nil, nil, fmt.Errorf("vmix request pad failed")
	}
	vBinSrc := vbin.GetStaticPad("src")
	if vBinSrc == nil {
		p.vmix.ReleaseRequestPad(vmixSink)
		_ = p.pipeline.Remove(vbin.Element)
		return nil, nil, fmt.Errorf("video bin: no src ghost pad")
	}
	if ret := vBinSrc.Link(vmixSink); ret != gst.PadLinkOK {
		p.vmix.ReleaseRequestPad(vmixSink)
		_ = p.pipeline.Remove(vbin.Element)
		return nil, nil, fmt.Errorf("link video bin → vmix: %v", ret)
	}
	// Дефолтные размеры на compositor.sink_N сразу — чтобы peer был виден
	// до того, как room вызовет applyLayout. Иначе compositor может зарендерить
	// peer с width=0/height=0 (невидимо).
	// zorder=10: явно ставим peer-pad ВЫШЕ testsrc-pad (zorder=0 по дефолту).
	// При равных zorder поведение compositor undefined — иногда нижний pad перекрывает верхний.
	e1 := vmixSink.SetProperty("xpos", int(0))
	e2 := vmixSink.SetProperty("ypos", int(0))
	e3 := vmixSink.SetProperty("width", int(scaledW))
	e4 := vmixSink.SetProperty("height", int(scaledH))
	e5 := vmixSink.SetProperty("zorder", uint(10))
	e6 := vmixSink.SetProperty("alpha", float64(1.0))
	fmt.Fprintf(os.Stderr, "default pad props peer=%s w=%d h=%d errs=%v/%v/%v/%v/%v/%v\n",
		peerID[:8], scaledW, scaledH, e1, e2, e3, e4, e5, e6)

	// --- audio branch ---
	// Явный format=S16LE на выходе — нужен чтобы VAD-appsink ниже декодировал samples
	// корректно (мы парсим как int16).
	aDesc := fmt.Sprintf(
		`appsrc name=asrc-%[1]s is-live=true format=time do-timestamp=true caps="application/x-rtp,media=audio,encoding-name=OPUS,clock-rate=48000,payload=111" `+
			`! rtpopusdepay `+
			`! queue max-size-buffers=32 leaky=downstream `+
			`! opusdec `+
			`! audioconvert `+
			`! audioresample `+
			`! audio/x-raw,format=S16LE,channels=2,rate=48000 `+
			`! queue max-size-buffers=8 leaky=downstream`,
		peerID)
	abin, err := gst.NewBinFromString(aDesc, true)
	if err != nil {
		p.vmix.ReleaseRequestPad(vmixSink)
		_ = p.pipeline.Remove(vbin.Element)
		return nil, nil, fmt.Errorf("audio bin parse: %w", err)
	}
	if err := p.pipeline.Add(abin.Element); err != nil {
		p.vmix.ReleaseRequestPad(vmixSink)
		_ = p.pipeline.Remove(vbin.Element)
		return nil, nil, fmt.Errorf("add audio bin: %w", err)
	}
	amixSink := p.amix.GetRequestPad("sink_%u")
	if amixSink == nil {
		_ = p.pipeline.Remove(abin.Element)
		p.vmix.ReleaseRequestPad(vmixSink)
		_ = p.pipeline.Remove(vbin.Element)
		return nil, nil, fmt.Errorf("amix request pad failed")
	}
	aBinSrc := abin.GetStaticPad("src")
	if aBinSrc == nil {
		p.amix.ReleaseRequestPad(amixSink)
		_ = p.pipeline.Remove(abin.Element)
		p.vmix.ReleaseRequestPad(vmixSink)
		_ = p.pipeline.Remove(vbin.Element)
		return nil, nil, fmt.Errorf("audio bin: no src pad")
	}

	// Вставляем tee между abin's src и amix.sink_%u: одна ветка → amix (для микширования),
	// вторая → appsink (для расчёта RMS в Go = VAD/ASD). GStreamer level элемент не работает
	// надёжно с go-gst bus reader'ом, поэтому считаем уровень сами по raw S16LE samples.
	teeStr := fmt.Sprintf(
		"tee name=vadtee-%s allow-not-linked=true "+
			"vadtee-%[1]s. ! queue max-size-buffers=4 leaky=downstream ! "+
			"appsink name=vadsink-%[1]s emit-signals=true sync=false max-buffers=4 drop=true "+
			"vadtee-%[1]s. ! queue max-size-buffers=4 leaky=downstream",
		peerID)
	teeBin, err := gst.NewBinFromString(teeStr, true)
	if err != nil {
		p.amix.ReleaseRequestPad(amixSink)
		_ = p.pipeline.Remove(abin.Element)
		p.vmix.ReleaseRequestPad(vmixSink)
		_ = p.pipeline.Remove(vbin.Element)
		return nil, nil, fmt.Errorf("vad-tee parse: %w", err)
	}
	if err := p.pipeline.Add(teeBin.Element); err != nil {
		p.amix.ReleaseRequestPad(amixSink)
		_ = p.pipeline.Remove(abin.Element)
		p.vmix.ReleaseRequestPad(vmixSink)
		_ = p.pipeline.Remove(vbin.Element)
		return nil, nil, fmt.Errorf("add vad-tee: %w", err)
	}
	teeSink := teeBin.GetStaticPad("sink")
	teeSrc := teeBin.GetStaticPad("src")
	if teeSink == nil || teeSrc == nil {
		_ = p.pipeline.Remove(teeBin.Element)
		p.amix.ReleaseRequestPad(amixSink)
		_ = p.pipeline.Remove(abin.Element)
		p.vmix.ReleaseRequestPad(vmixSink)
		_ = p.pipeline.Remove(vbin.Element)
		return nil, nil, fmt.Errorf("vad-tee pads missing")
	}
	if ret := aBinSrc.Link(teeSink); ret != gst.PadLinkOK {
		_ = p.pipeline.Remove(teeBin.Element)
		p.amix.ReleaseRequestPad(amixSink)
		_ = p.pipeline.Remove(abin.Element)
		p.vmix.ReleaseRequestPad(vmixSink)
		_ = p.pipeline.Remove(vbin.Element)
		return nil, nil, fmt.Errorf("link abin → vad-tee: %v", ret)
	}
	if ret := teeSrc.Link(amixSink); ret != gst.PadLinkOK {
		_ = p.pipeline.Remove(teeBin.Element)
		p.amix.ReleaseRequestPad(amixSink)
		_ = p.pipeline.Remove(abin.Element)
		p.vmix.ReleaseRequestPad(vmixSink)
		_ = p.pipeline.Remove(vbin.Element)
		return nil, nil, fmt.Errorf("link vad-tee → amix: %v", ret)
	}
	// Получаем VAD appsink и подключаем callback для расчёта RMS.
	vadSinkEl, err := teeBin.GetElementByName("vadsink-" + peerID)
	if err != nil || vadSinkEl == nil {
		return nil, nil, fmt.Errorf("vadsink not found")
	}
	vadSink := app.SinkFromElement(vadSinkEl)
	vadSink.SetCallbacks(&app.SinkCallbacks{
		NewSampleFunc: func(sink *app.Sink) gst.FlowReturn {
			return p.onVADSample(peerID, sink)
		},
	})
	fmt.Fprintf(os.Stderr, "VAD tee+appsink added for peer=%s\n", peerID[:8])

	// appsrc-ы внутри bin'ов
	vsrcEl, err := vbin.GetElementByName("vsrc-" + peerID)
	if err != nil || vsrcEl == nil {
		return nil, nil, fmt.Errorf("vsrc not found in bin: %v", err)
	}
	asrcEl, err := abin.GetElementByName("asrc-" + peerID)
	if err != nil || asrcEl == nil {
		return nil, nil, fmt.Errorf("asrc not found in bin: %v", err)
	}
	vsrc := app.SrcFromElement(vsrcEl)
	asrc := app.SrcFromElement(asrcEl)

	branch := &peerBranch{
		id:       peerID,
		videoBin: vbin,
		audioBin: abin,
		videoSrc: vsrc,
		audioSrc: asrc,
		vMixPad:  vmixSink,
		aMixPad:  amixSink,
		teeBin:   teeBin,
		videoCh:  make(chan []byte, 256),
		audioCh:  make(chan []byte, 256),
		closeCh:  make(chan struct{}),
	}

	if ok := vbin.SyncStateWithParent(); !ok {
		fmt.Fprintln(os.Stderr, "sync vbin state failed")
	}
	if ok := abin.SyncStateWithParent(); !ok {
		fmt.Fprintln(os.Stderr, "sync abin state failed")
	}
	if ok := teeBin.SyncStateWithParent(); !ok {
		fmt.Fprintln(os.Stderr, "sync vad-tee state failed")
	}

	// level вынесен на pipeline уровень — element messages теперь приходят прямо в pipeline
	// bus, без отдельного per-bin reader'а.
	// Диагностика: какое состояние у bin'ов через секунду.
	go func(pid string, vb, ab *gst.Bin) {
		time.Sleep(1500 * time.Millisecond)
		fmt.Fprintf(os.Stderr, "bin states peer=%s vbin=%s abin=%s pipeline=%s\n",
			pid, vb.GetCurrentState(), ab.GetCurrentState(), p.pipeline.GetCurrentState())
	}(peerID[:8], vbin, abin)

	go p.pumpAppsrc(vsrc, branch.videoCh, branch.closeCh, "video-"+peerID[:8])
	go p.pumpAppsrc(asrc, branch.audioCh, branch.closeCh, "audio-"+peerID[:8])

	p.mu.Lock()
	p.peers[peerID] = branch
	p.mu.Unlock()

	return branch.videoCh, branch.audioCh, nil
}

func (p *gstPipeline) RemovePeer(peerID string) error {
	p.mu.Lock()
	b, ok := p.peers[peerID]
	if !ok {
		p.mu.Unlock()
		return nil
	}
	delete(p.peers, peerID)
	p.mu.Unlock()
	p.disposePeer(b)
	return nil
}

func (p *gstPipeline) disposePeer(b *peerBranch) {
	select {
	case <-b.closeCh:
	default:
		close(b.closeCh)
	}
	if b.videoSrc != nil {
		b.videoSrc.EndStream()
	}
	if b.audioSrc != nil {
		b.audioSrc.EndStream()
	}
	if b.vMixPad != nil && p.vmix != nil {
		p.vmix.ReleaseRequestPad(b.vMixPad)
	}
	if b.aMixPad != nil && p.amix != nil {
		p.amix.ReleaseRequestPad(b.aMixPad)
	}
	if b.videoBin != nil {
		_ = b.videoBin.SetState(gst.StateNull)
		_ = p.pipeline.Remove(b.videoBin.Element)
	}
	if b.audioBin != nil {
		_ = b.audioBin.SetState(gst.StateNull)
		_ = p.pipeline.Remove(b.audioBin.Element)
	}
	if b.teeBin != nil {
		_ = b.teeBin.SetState(gst.StateNull)
		_ = p.pipeline.Remove(b.teeBin.Element)
	}
}

// onVADSample вызывается на каждый decoded audio buffer для peer'а.
// Считаем RMS (S16LE → float64 → log10) и пушим в asdCh.
func (p *gstPipeline) onVADSample(peerID string, sink *app.Sink) gst.FlowReturn {
	sample := sink.PullSample()
	if sample == nil {
		return gst.FlowOK
	}
	buf := sample.GetBuffer()
	if buf == nil {
		return gst.FlowOK
	}
	mapInfo := buf.Map(gst.MapRead)
	if mapInfo == nil {
		return gst.FlowOK
	}
	data := mapInfo.Bytes()
	buf.Unmap()
	// S16LE: каждый sample = 2 байта, little endian; stereo = чередуются L,R.
	if len(data) < 4 {
		return gst.FlowOK
	}
	var sumSq float64
	n := 0
	for i := 0; i+1 < len(data); i += 2 {
		v := int16(uint16(data[i]) | uint16(data[i+1])<<8)
		f := float64(v) / 32768.0
		sumSq += f * f
		n++
	}
	if n == 0 {
		return gst.FlowOK
	}
	rms := sumSq / float64(n)
	dbfs := -90.0
	if rms > 1e-10 {
		dbfs = 10 * math.Log10(rms)
	}
	select {
	case p.asdCh <- ASDLevel{PeerID: peerID, DBFS: dbfs}:
	default:
	}
	return gst.FlowOK
}

func (p *gstPipeline) pumpAppsrc(src *app.Source, in chan []byte, done chan struct{}, kind string) {
	var pushed atomic.Uint64
	for {
		select {
		case <-done:
			fmt.Fprintf(os.Stderr, "pump %s done, pushed=%d\n", kind, pushed.Load())
			return
		case pkt, ok := <-in:
			if !ok {
				fmt.Fprintf(os.Stderr, "pump %s channel closed, pushed=%d\n", kind, pushed.Load())
				return
			}
			if len(pkt) == 0 {
				continue
			}
			buf := gst.NewBufferFromBytes(pkt)
			if buf == nil {
				continue
			}
			ret := src.PushBuffer(buf)
			if ret != gst.FlowOK {
				fmt.Fprintf(os.Stderr, "pump %s PushBuffer ret=%v, pushed=%d — bailing\n", kind, ret, pushed.Load())
				return
			}
			n := pushed.Add(1)
			if n == 1 || n%500 == 0 {
				fmt.Fprintf(os.Stderr, "pump %s pushed=%d (last len=%d)\n", kind, n, len(pkt))
			}
		}
	}
}

func (p *gstPipeline) VideoOut() <-chan Sample   { return p.videoOut }
func (p *gstPipeline) AudioOut() <-chan Sample   { return p.audioOut }
func (p *gstPipeline) ASDLevel() <-chan ASDLevel { return p.asdCh }

// handleLevelMessage парсит bus-сообщение от level элемента и отправляет уровень в asdCh.
// level emit'ит structure "level" с массивами rms/peak/decay (dBFS, отрицательные значения).
// msg.Source() = "level-<peerID>" — оттуда извлекаем кому принадлежит этот уровень.
func (p *gstPipeline) handleLevelMessage(msg *gst.Message) {
	src := msg.Source()
	if !strings.HasPrefix(src, "level-") {
		return
	}
	peerID := src[len("level-"):]
	s := msg.GetStructure()
	if s == nil || s.Name() != "level" {
		return
	}
	val, err := s.GetValue("rms")
	if err != nil {
		return
	}
	floats, ok := val.([]interface{})
	if !ok {
		return
	}
	if len(floats) == 0 {
		return
	}
	// усредняем RMS по каналам; берём максимум, чтобы громкий канал не маскировался тихим
	var maxDB float64 = -200
	for _, f := range floats {
		if v, ok := f.(float64); ok && v > maxDB {
			maxDB = v
		}
	}
	if maxDB <= -200 {
		return
	}
	select {
	case p.asdCh <- ASDLevel{PeerID: peerID, DBFS: maxDB}:
	default:
	}
}

var pulledV, pulledA atomic.Uint64

func (p *gstPipeline) onVideoSample(sink *app.Sink) gst.FlowReturn {
	n := pulledV.Add(1)
	if n == 1 || n%500 == 0 {
		fmt.Fprintf(os.Stderr, "appsink video new-sample n=%d\n", n)
	}
	return p.pullSample(sink, p.videoOut)
}

func (p *gstPipeline) onAudioSample(sink *app.Sink) gst.FlowReturn {
	n := pulledA.Add(1)
	if n == 1 || n%500 == 0 {
		fmt.Fprintf(os.Stderr, "appsink audio new-sample n=%d\n", n)
	}
	return p.pullSample(sink, p.audioOut)
}

func (p *gstPipeline) pullSample(sink *app.Sink, out chan Sample) gst.FlowReturn {
	sample := sink.PullSample()
	if sample == nil {
		return gst.FlowOK
	}
	buf := sample.GetBuffer()
	if buf == nil {
		return gst.FlowOK
	}
	mapInfo := buf.Map(gst.MapRead)
	if mapInfo == nil {
		return gst.FlowOK
	}
	src := mapInfo.Bytes()
	data := make([]byte, len(src))
	copy(data, src)
	buf.Unmap()
	select {
	case out <- Sample{Data: data}:
	default:
		// drop, чтобы encoder не блокировался
	}
	return gst.FlowOK
}

func (p *gstPipeline) UpdateLayout(cells []layout.Cell) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	fmt.Fprintf(os.Stderr, "UpdateLayout called with %d cells, %d peers in pipeline\n", len(cells), len(p.peers))

	// Шаг 1: прячем ВСЕ placeholder-pad'ы за пределы canvas.
	for i := 0; i < 20; i++ {
		if p.placeholderPads[i] != nil {
			_ = p.placeholderPads[i].SetProperty("xpos", int(-9999))
			_ = p.placeholderPads[i].SetProperty("ypos", int(-9999))
			_ = p.placeholderPads[i].SetProperty("width", int(1))
			_ = p.placeholderPads[i].SetProperty("height", int(1))
		}
	}

	// Шаг 2: применяем cells. Для каждой ячейки:
	//   - если SlotIndex 0..19 — позиционируем соответствующий placeholder-pad в координаты cell
	//     с zorder=5; при отсутствии peer'а пользователь увидит "No user" в этой клетке.
	//   - если PeerID != "" — peer-pad с zorder=10+c.Z поверх placeholder.
	for _, c := range cells {
		if c.SlotIndex >= 0 && c.SlotIndex < 20 && p.placeholderPads[c.SlotIndex] != nil {
			ph := p.placeholderPads[c.SlotIndex]
			_ = ph.SetProperty("xpos", int(c.X))
			_ = ph.SetProperty("ypos", int(c.Y))
			_ = ph.SetProperty("width", int(c.W))
			_ = ph.SetProperty("height", int(c.H))
			_ = ph.SetProperty("zorder", uint(5))
		}
		if c.PeerID == "" {
			continue
		}
		b, ok := p.peers[c.PeerID]
		if !ok || b.vMixPad == nil {
			fmt.Fprintf(os.Stderr, "  cell peer=%s: not found in pipeline (ok=%v)\n", c.PeerID[:8], ok)
			continue
		}
		e1 := b.vMixPad.SetProperty("xpos", int(c.X))
		e2 := b.vMixPad.SetProperty("ypos", int(c.Y))
		e3 := b.vMixPad.SetProperty("width", int(c.W))
		e4 := b.vMixPad.SetProperty("height", int(c.H))
		e5 := b.vMixPad.SetProperty("zorder", uint(10+c.Z))
		fmt.Fprintf(os.Stderr, "  cell peer=%s slot=%d: x=%v y=%v w=%v h=%v z=%v errs=%v/%v/%v/%v/%v\n",
			c.PeerID[:8], c.SlotIndex, c.X, c.Y, c.W, c.H, c.Z, e1, e2, e3, e4, e5)
	}
	return nil
}

func (p *gstPipeline) StartRecording(filename string) error {
	if p.fsink == nil || p.recValveV == nil || p.recValveA == nil {
		return fmt.Errorf("recording branch not present in pipeline")
	}
	if filename == "" {
		return fmt.Errorf("filename required")
	}
	if err := os.MkdirAll(filepath.Dir(filename), 0o755); err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.recRunning {
		return nil
	}
	if err := p.fsink.SetProperty("location", filename); err != nil {
		return fmt.Errorf("set filesink location: %w", err)
	}
	_ = p.recValveV.SetProperty("drop", false)
	_ = p.recValveA.SetProperty("drop", false)
	p.recRunning = true
	p.recPath = filename
	return nil
}

func (p *gstPipeline) StopRecording() (string, int64, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.recRunning {
		return "", 0, nil
	}
	if p.recValveV != nil {
		_ = p.recValveV.SetProperty("drop", true)
	}
	if p.recValveA != nil {
		_ = p.recValveA.SetProperty("drop", true)
	}
	p.recRunning = false
	path := p.recPath
	var size int64
	if st, err := os.Stat(path); err == nil {
		size = st.Size()
	}
	return path, size, nil
}

// ----- pipeline description ------------------------------------------------

func buildPipelineDesc(spec Spec) string {
	videoEnc := buildVideoEnc(spec)
	audioEnc := buildAudioEnc(spec)
	// КЛЮЧЕВОЕ: videotestsrc/audiotestsrc-заглушки нужны чтобы compositor/audiomixer
	// имели постоянный live-input и pipeline перешёл из READY в PLAYING.
	// Peer-ветки подцепляются как compositor.sink_5+ поверх sink_0 (фон) и sink_1..sink_4 (placeholders).
	//
	// Placeholder для пустых фреймов: 1 общий источник (videotestsrc + textoverlay "No user")
	// разветвляется через tee на 20 веток → compositor.sink_1..sink_20.
	// Один source вместо 20 отдельных videotestsrc — экономит CPU и память. При applyLayout
	// placeholder pad N позиционируется в координаты cell.slot-N с zorder=5; если peer занимает
	// slot N, его pad с zorder=10 рисуется поверх и скрывает placeholder.
	placeholderTee := "videotestsrc pattern=solid-color foreground-color=0xff181820 is-live=true " +
		"! video/x-raw,format=I420,width=320,height=180,framerate=30/1,pixel-aspect-ratio=1/1 " +
		"! textoverlay text=\"No user\" valignment=center halignment=center font-desc=\"Sans Bold 24\" " +
		"! tee name=phtee allow-not-linked=true "
	var phBranches string
	for i := 0; i < 20; i++ {
		phBranches += "phtee. ! queue max-size-buffers=4 leaky=downstream ! vmix. "
	}
	return fmt.Sprintf(
		"videotestsrc pattern=black is-live=true do-timestamp=true "+
			"! video/x-raw,format=I420,width=%[1]d,height=%[2]d,framerate=%[3]d/1,pixel-aspect-ratio=1/1 "+
			"! compositor name=vmix latency=100000000 background=black "+
			"! video/x-raw,format=I420,width=%[1]d,height=%[2]d,framerate=%[3]d/1,pixel-aspect-ratio=1/1 "+
			"! tee name=vtee "+
			placeholderTee+phBranches+
			"vtee. ! queue max-size-buffers=4 leaky=downstream ! %[4]s ! appsink name=video-sink sync=false emit-signals=false max-buffers=4 drop=true "+
			"vtee. ! queue ! valve name=recv drop=true ! videoconvert ! x264enc tune=zerolatency speed-preset=ultrafast bitrate=2500 ! mp4mux name=mp4 ! filesink name=fsink location=/dev/null async=false "+
			"audiotestsrc volume=0.0 is-live=true do-timestamp=true wave=silence "+
			"! audio/x-raw,format=S16LE,channels=2,rate=48000 "+
			"! audiomixer name=amix latency=300000000 force-live=true ignore-inactive-pads=true "+
			"! audio/x-raw,format=S16LE,channels=2,rate=48000 "+
			"! tee name=atee "+
			"atee. ! queue max-size-buffers=8 leaky=downstream ! %[5]s ! appsink name=audio-sink sync=false emit-signals=false max-buffers=8 drop=true "+
			"atee. ! queue ! valve name=reca drop=true ! audioconvert ! voaacenc ! mp4.",
		spec.Width, spec.Height, spec.FPS, videoEnc, audioEnc)
}

func buildVideoEnc(spec Spec) string {
	if spec.VideoCodec == "h264" {
		return "videoconvert ! x264enc tune=zerolatency speed-preset=ultrafast bitrate=" + itoa(spec.VideoBitrate/1000) +
			" key-int-max=" + itoa(spec.FPS*2) + " ! rtph264pay pt=102 config-interval=-1 ssrc=1"
	}
	return "videoconvert ! vp8enc deadline=1 cpu-used=8 target-bitrate=" + itoa(spec.VideoBitrate) +
		" keyframe-max-dist=" + itoa(spec.FPS*2) + " ! rtpvp8pay pt=96 ssrc=1"
}

func buildAudioEnc(spec Spec) string {
	return "audioconvert ! audioresample ! opusenc bitrate=" + itoa(spec.AudioBitrate) +
		" frame-size=20 ! rtpopuspay pt=111 ssrc=2"
}

func itoa(i int) string { return strconv.Itoa(i) }
