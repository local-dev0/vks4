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

	vmix     *gst.Element
	amix     *gst.Element
	vsink    *app.Sink
	asink    *app.Sink
	vsinkSIP *app.Sink // appsink для SIP H.264 егресса
	asinkSIP *app.Sink // appsink для SIP PCMU егресса

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

	videoOut    chan Sample
	audioOut    chan Sample
	sipVideoOut chan Sample
	sipAudioOut chan Sample
	asdCh       chan ASDLevel

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
	teeBin   *gst.Bin    // vad tee + appsink
	nameTov  *gst.Element // textoverlay для подписи peer'а (в pipeline root, не в bin)
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
		// 500 kbps достаточно для MCU 1280×720 mix при 30fps с VP8.
		// Понижено с 800: серверу не нужна студийная битрейт-картинка, клиент всё равно
		// смотрит уменьшенно. Снижает downlink и нагрузку на encoder.
		spec.VideoBitrate = 500_000
	}
	if spec.AudioBitrate == 0 {
		spec.AudioBitrate = 48_000
	}

	desc := buildPipelineDesc(spec)
	pl, err := gst.NewPipelineFromString(desc)
	if err != nil {
		return nil, fmt.Errorf("parse pipeline: %w", err)
	}

	g := &gstPipeline{
		spec:        spec,
		pipeline:    pl,
		peers:       map[string]*peerBranch{},
		videoOut:    make(chan Sample, 32),
		audioOut:    make(chan Sample, 64),
		sipVideoOut: make(chan Sample, 32),
		sipAudioOut: make(chan Sample, 64),
		asdCh:       make(chan ASDLevel, 128),
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

	// SIP video sink (H.264 для Polycom) — отдельная ветка vtee → x264enc → rtph264pay.
	if sipVsinkEl, _ := pl.GetElementByName("sip-video-sink"); sipVsinkEl != nil {
		g.vsinkSIP = app.SinkFromElement(sipVsinkEl)
		g.vsinkSIP.SetCallbacks(&app.SinkCallbacks{NewSampleFunc: g.onSIPVideoSample})
	}
	// SIP audio sink (PCMU для Polycom) — отдельная ветка atee → mulawenc → rtppcmupay.
	if sipAsinkEl, _ := pl.GetElementByName("sip-audio-sink"); sipAsinkEl != nil {
		g.asinkSIP = app.SinkFromElement(sipAsinkEl)
		g.asinkSIP.SetCallbacks(&app.SinkCallbacks{NewSampleFunc: g.onSIPAudioSample})
	}

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

func (p *gstPipeline) AddPeer(peerID, videoCodec, audioCodec string) (chan<- []byte, chan<- []byte, error) {
	p.mu.Lock()
	if _, ok := p.peers[peerID]; ok {
		p.mu.Unlock()
		return nil, nil, fmt.Errorf("peer %s already exists", peerID)
	}
	p.mu.Unlock()

	// GStreamer parse-launch использует "." как разделитель element.pad, "-" парсится как
	// арифметика в pad-reference. Polycom call-id может содержать "@host.ip" — все спецсимволы
	// заменяем на "_", оставляем только [a-zA-Z0-9_].
	gstName := sanitizeName(peerID)

	// --- video branch ---
	// avdec_vp8/avdec_h264 (libav) терпимее к damaged input чем нативные libvpx/libx264 при потерях.
	scaledW, scaledH := p.spec.Width/2, p.spec.Height/2
	// rtp caps: encoding-name + payload должны соответствовать SDP'у источника:
	//   VP8 — WebRTC (Pion negotiates PT=96)
	//   H264 — SIP (sip-gateway отвечает PT=109 в SDP к FreeSWITCH)
	var rtpCaps, depay, dec string
	switch videoCodec {
	case "h264":
		rtpCaps = `caps="application/x-rtp,media=video,encoding-name=H264,clock-rate=90000,payload=109"`
		depay = "rtph264depay"
		dec = "avdec_h264"
	default: // vp8
		rtpCaps = `caps="application/x-rtp,media=video,encoding-name=VP8,clock-rate=90000,payload=96"`
		depay = "rtpvp8depay"
		dec = "avdec_vp8"
	}
	// КЛЮЧЕВОЕ: явно фиксируем format=I420 — иначе compositor.sink_%u не negotiate
	// и весь bin падает в streaming stopped, reason not-negotiated (-4).
	vDesc := fmt.Sprintf(
		`appsrc name=vsrc_%[1]s is-live=true format=time do-timestamp=true %[4]s `+
			`! %[5]s `+
			`! queue max-size-buffers=32 leaky=downstream `+
			`! %[6]s `+
			`! videoconvert `+
			`! videoscale add-borders=true `+
			`! videorate `+
			`! video/x-raw,format=I420,width=%[2]d,height=%[3]d,framerate=30/1,pixel-aspect-ratio=1/1 `+
			`! queue max-size-buffers=4 leaky=downstream`,
		gstName, scaledW, scaledH, rtpCaps, depay, dec)

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
	// Вставляем textoverlay между peer's video bin и compositor (вне bin'а, в pipeline root).
	// Текст задаётся позже через SetPeerName. Внутри bin textoverlay ломал caps negotiation,
	// здесь он работает как обычный element pipeline.
	nameTov, err := gst.NewElementWithName("textoverlay", "tov_"+gstName)
	if err != nil || nameTov == nil {
		p.vmix.ReleaseRequestPad(vmixSink)
		_ = p.pipeline.Remove(vbin.Element)
		return nil, nil, fmt.Errorf("create textoverlay: %w", err)
	}
	_ = nameTov.SetProperty("text", " ")
	_ = nameTov.SetProperty("valignment", "bottom")
	_ = nameTov.SetProperty("halignment", "center")
	_ = nameTov.SetProperty("font-desc", "Sans Bold 14")
	_ = nameTov.SetProperty("shaded-background", true)
	_ = nameTov.SetProperty("shading-value", uint(160))
	_ = nameTov.SetProperty("ypad", int(6))
	if err := p.pipeline.Add(nameTov); err != nil {
		p.vmix.ReleaseRequestPad(vmixSink)
		_ = p.pipeline.Remove(vbin.Element)
		return nil, nil, fmt.Errorf("add textoverlay: %w", err)
	}
	tovSink := nameTov.GetStaticPad("video_sink")
	tovSrc := nameTov.GetStaticPad("src")
	if tovSink == nil || tovSrc == nil {
		_ = p.pipeline.Remove(nameTov)
		p.vmix.ReleaseRequestPad(vmixSink)
		_ = p.pipeline.Remove(vbin.Element)
		return nil, nil, fmt.Errorf("textoverlay pads missing")
	}
	if ret := vBinSrc.Link(tovSink); ret != gst.PadLinkOK {
		_ = p.pipeline.Remove(nameTov)
		p.vmix.ReleaseRequestPad(vmixSink)
		_ = p.pipeline.Remove(vbin.Element)
		return nil, nil, fmt.Errorf("link vbin → textoverlay: %v", ret)
	}
	if ret := tovSrc.Link(vmixSink); ret != gst.PadLinkOK {
		_ = p.pipeline.Remove(nameTov)
		p.vmix.ReleaseRequestPad(vmixSink)
		_ = p.pipeline.Remove(vbin.Element)
		return nil, nil, fmt.Errorf("link textoverlay → vmix: %v", ret)
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
	_, _, _, _, _, _ = e1, e2, e3, e4, e5, e6

	// --- audio branch ---
	// Явный format=S16LE на выходе — нужен чтобы VAD-appsink ниже декодировал samples
	// корректно (мы парсим как int16).
	// audioCodec: "opus" (default, WebRTC) или "pcmu" (Polycom/legacy SIP).
	var aRtpCaps, aDepay, aDec string
	switch audioCodec {
	case "pcmu":
		aRtpCaps = `caps="application/x-rtp,media=audio,encoding-name=PCMU,clock-rate=8000,payload=0"`
		aDepay = "rtppcmudepay"
		aDec = "mulawdec"
	default: // opus
		aRtpCaps = `caps="application/x-rtp,media=audio,encoding-name=OPUS,clock-rate=48000,payload=111"`
		aDepay = "rtpopusdepay"
		aDec = "opusdec"
	}
	aDesc := fmt.Sprintf(
		`appsrc name=asrc_%[1]s is-live=true format=time do-timestamp=true %[2]s `+
			`! %[3]s `+
			`! queue max-size-buffers=32 leaky=downstream `+
			`! %[4]s `+
			`! audioconvert `+
			`! audioresample `+
			`! audio/x-raw,format=S16LE,channels=2,rate=48000 `+
			`! queue max-size-buffers=8 leaky=downstream`,
		gstName, aRtpCaps, aDepay, aDec)
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
		"tee name=vadtee_%s allow-not-linked=true "+
			"vadtee_%[1]s. ! queue max-size-buffers=4 leaky=downstream ! "+
			"appsink name=vadsink_%[1]s emit-signals=true sync=false max-buffers=4 drop=true "+
			"vadtee_%[1]s. ! queue max-size-buffers=4 leaky=downstream",
		gstName)
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
	vadSinkEl, err := teeBin.GetElementByName("vadsink_" + gstName)
	if err != nil || vadSinkEl == nil {
		return nil, nil, fmt.Errorf("vadsink not found")
	}
	vadSink := app.SinkFromElement(vadSinkEl)
	vadSink.SetCallbacks(&app.SinkCallbacks{
		NewSampleFunc: func(sink *app.Sink) gst.FlowReturn {
			return p.onVADSample(peerID, sink)
		},
	})

	// appsrc-ы внутри bin'ов
	vsrcEl, err := vbin.GetElementByName("vsrc_" + gstName)
	if err != nil || vsrcEl == nil {
		return nil, nil, fmt.Errorf("vsrc not found in bin: %v", err)
	}
	asrcEl, err := abin.GetElementByName("asrc_" + gstName)
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
		nameTov:  nameTov,
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
	if ok := nameTov.SyncStateWithParent(); !ok {
		fmt.Fprintln(os.Stderr, "sync textoverlay state failed")
	}

	// level вынесен на pipeline уровень — element messages теперь приходят прямо в pipeline
	// bus, без отдельного per-bin reader'а.
	// Диагностика: какое состояние у bin'ов через секунду.
	go func(pid string, vb, ab *gst.Bin) {
		time.Sleep(1500 * time.Millisecond)
		_, _, _, _ = pid, vb, ab, p.pipeline
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

	// 1) Останавливаем ВСЕ peer-элементы одновременно (StateNull). Это размораживает
	// downstream queue/aggregator pads и предотвращает "streaming stopped reason not-linked".
	if b.videoBin != nil {
		_ = b.videoBin.SetState(gst.StateNull)
	}
	if b.audioBin != nil {
		_ = b.audioBin.SetState(gst.StateNull)
	}
	if b.teeBin != nil {
		_ = b.teeBin.SetState(gst.StateNull)
	}
	if b.nameTov != nil {
		_ = b.nameTov.SetState(gst.StateNull)
	}

	// 2) Освобождаем request-pads у compositor и audiomixer (peer-pad position в них больше не нужен).
	if b.vMixPad != nil && p.vmix != nil {
		p.vmix.ReleaseRequestPad(b.vMixPad)
	}
	if b.aMixPad != nil && p.amix != nil {
		p.amix.ReleaseRequestPad(b.aMixPad)
	}

	// 3) Удаляем элементы из pipeline.
	if b.nameTov != nil {
		_ = p.pipeline.Remove(b.nameTov)
	}
	if b.teeBin != nil {
		_ = p.pipeline.Remove(b.teeBin.Element)
	}
	if b.videoBin != nil {
		_ = p.pipeline.Remove(b.videoBin.Element)
	}
	if b.audioBin != nil {
		_ = p.pipeline.Remove(b.audioBin.Element)
	}
}

// vadCallCount для диагностики: должен инкрементироваться при разговоре.
var vadCallCount atomic.Uint64

// onVADSample вызывается на каждый decoded audio buffer для peer'а.
// Считаем RMS (S16LE → float64 → log10) и пушим в asdCh.
func (p *gstPipeline) onVADSample(peerID string, sink *app.Sink) gst.FlowReturn {
	if n := vadCallCount.Add(1); n == 1 || n%200 == 0 {
		fmt.Fprintf(os.Stderr, "VAD onSample n=%d peer=%s\n", n, peerID[:8])
	}
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
			pushed.Add(1)
		}
	}
}

func (p *gstPipeline) VideoOut() <-chan Sample    { return p.videoOut }
func (p *gstPipeline) AudioOut() <-chan Sample    { return p.audioOut }
func (p *gstPipeline) SIPVideoOut() <-chan Sample { return p.sipVideoOut }
func (p *gstPipeline) SIPAudioOut() <-chan Sample { return p.sipAudioOut }
func (p *gstPipeline) ASDLevel() <-chan ASDLevel  { return p.asdCh }

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
	pulledV.Add(1)
	return p.pullSample(sink, p.videoOut)
}

func (p *gstPipeline) onAudioSample(sink *app.Sink) gst.FlowReturn {
	pulledA.Add(1)
	return p.pullSample(sink, p.audioOut)
}

func (p *gstPipeline) onSIPVideoSample(sink *app.Sink) gst.FlowReturn {
	return p.pullSample(sink, p.sipVideoOut)
}

func (p *gstPipeline) onSIPAudioSample(sink *app.Sink) gst.FlowReturn {
	return p.pullSample(sink, p.sipAudioOut)
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

// SetPeerOverlay полностью устанавливает overlay peer'а — markup и базовые свойства.
// markup="" → пустой overlay (text="", shaded-background=false).
// markup non-empty → text=markup, use-markup=true (Pango markup для bg/fg/alpha).
func (p *gstPipeline) SetPeerOverlay(peerID, markup string, fontSize int) error {
	p.mu.Lock()
	b, ok := p.peers[peerID]
	p.mu.Unlock()
	if !ok || b == nil || b.nameTov == nil {
		return fmt.Errorf("peer %s textoverlay not found", peerID[:8])
	}
	if markup == "" {
		_ = b.nameTov.SetProperty("text", "")
		_ = b.nameTov.SetProperty("shaded-background", false)
		return nil
	}
	if fontSize < 8 {
		fontSize = 14
	}
	_ = b.nameTov.SetProperty("shaded-background", false) // фон рисуем через markup
	_ = b.nameTov.SetProperty("use-markup", true)
	_ = b.nameTov.SetProperty("font-desc", fmt.Sprintf("Sans Bold %d", fontSize))
	return b.nameTov.SetProperty("text", markup)
}

// SetPeerAudioMute мьютит aMixPad этого peer'а у audiomixer — peer перестаёт
// контрибутить в общий mix output, но VAD-ветка (tee → vadsink) продолжает
// работать (decode/levels считаются как раньше).
func (p *gstPipeline) SetPeerAudioMute(peerID string, muted bool) error {
	p.mu.Lock()
	b, ok := p.peers[peerID]
	p.mu.Unlock()
	if !ok || b == nil || b.aMixPad == nil {
		return fmt.Errorf("peer %s amix pad not found", peerID[:8])
	}
	return b.aMixPad.SetProperty("mute", muted)
}

// SetPeerName / SetPeerStyle оставлены для совместимости с интерфейсом, делегируют room'у
// (room сам формирует markup и зовёт SetPeerOverlay).
func (p *gstPipeline) SetPeerName(peerID, name string) error {
	// Сюда room передаёт уже готовый markup. Если пусто — отключаем overlay.
	return p.SetPeerOverlay(peerID, name, 14)
}

func (p *gstPipeline) SetPeerStyle(peerID string, bgAlpha float64, fontSize int, fontColor string) error {
	_, _, _, _ = peerID, bgAlpha, fontSize, fontColor
	// no-op: стиль рендерится через markup в SetPeerName/SetPeerOverlay,
	// конкретные значения подставляет room.
	return nil
}

func (p *gstPipeline) UpdateLayout(cells []layout.Cell) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	_ = cells // UpdateLayout вызывается часто (по ASD); подробный лог раньше шёл в loki

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
		_ = e1
		_ = e2
		_ = e3
		_ = e4
		_ = e5
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
			// Параллельная H.264 ветка для SIP-egress (Polycom не умеет VP8).
			// PT=109 совпадает с тем что sip-gateway оффрит в SDP answer.
			// profile=baseline — Polycom надёжно декодирует только baseline (не main/high).
			// h264parse config-interval=1 + rtph264pay config-interval=1 — SPS/PPS в каждый
			// keyframe (без них Polycom не может инициализировать декодер: в SDP fmtp нет
			// sprop-parameter-sets, поэтому SPS/PPS должны приходить в RTP).
			"vtee. ! queue max-size-buffers=4 leaky=downstream ! videoconvert ! x264enc tune=zerolatency speed-preset=ultrafast bitrate=1500 key-int-max=60 ! video/x-h264,profile=baseline ! h264parse config-interval=1 ! rtph264pay pt=109 config-interval=1 ssrc=3 ! appsink name=sip-video-sink sync=false emit-signals=false max-buffers=4 drop=true "+
			"vtee. ! queue ! valve name=recv drop=true ! videoconvert ! x264enc tune=zerolatency speed-preset=ultrafast bitrate=2500 ! mp4mux name=mp4 ! filesink name=fsink location=/dev/null async=false "+
			"audiotestsrc volume=0.0 is-live=true do-timestamp=true wave=silence "+
			"! audio/x-raw,format=S16LE,channels=2,rate=48000 "+
			"! audiomixer name=amix latency=300000000 force-live=true ignore-inactive-pads=true "+
			"! audio/x-raw,format=S16LE,channels=2,rate=48000 "+
			"! tee name=atee "+
			"atee. ! queue max-size-buffers=8 leaky=downstream ! %[5]s ! appsink name=audio-sink sync=false emit-signals=false max-buffers=8 drop=true "+
			// Параллельная PCMU ветка для SIP-egress (Polycom не умеет Opus).
			// 8 kHz mono — нативная частота PCMU/G.711. PT=0 совпадает с SDP answer.
			"atee. ! queue max-size-buffers=8 leaky=downstream ! audioconvert ! audioresample ! audio/x-raw,format=S16LE,channels=1,rate=8000 ! mulawenc ! rtppcmupay pt=0 ssrc=4 ! appsink name=sip-audio-sink sync=false emit-signals=false max-buffers=8 drop=true "+
			"atee. ! queue ! valve name=reca drop=true ! audioconvert ! voaacenc ! mp4.",
		spec.Width, spec.Height, spec.FPS, videoEnc, audioEnc)
}

func buildVideoEnc(spec Spec) string {
	if spec.VideoCodec == "h264" {
		// vbv-buf-capacity + bitrate-tolerance держат outgoing близко к target.
		return "videoconvert ! x264enc tune=zerolatency speed-preset=ultrafast bitrate=" + itoa(spec.VideoBitrate/1000) +
			" key-int-max=" + itoa(spec.FPS*2) + " vbv-buf-capacity=600 ! rtph264pay pt=102 config-interval=-1 ssrc=1"
	}
	// min-quantizer/max-quantizer ужесточают bitrate cap для VP8 (без них target-bitrate
	// это soft hint и encoder может выдавать 2-3× выше при complex scenes).
	return "videoconvert ! vp8enc deadline=1 cpu-used=8 target-bitrate=" + itoa(spec.VideoBitrate) +
		" min-quantizer=15 max-quantizer=50 buffer-size=600 buffer-initial-size=400 buffer-optimal-size=500 " +
		"keyframe-max-dist=" + itoa(spec.FPS*2) + " end-usage=cbr ! rtpvp8pay pt=96 ssrc=1"
}

func buildAudioEnc(spec Spec) string {
	return "audioconvert ! audioresample ! opusenc bitrate=" + itoa(spec.AudioBitrate) +
		" frame-size=20 ! rtpopuspay pt=111 ssrc=2"
}

func itoa(i int) string { return strconv.Itoa(i) }

// sanitizeName заменяет всё кроме [a-zA-Z0-9_] на "_" — чтобы передавать peerID
// безопасно в gst-parse-launch строки (имена элементов и pad-references).
func sanitizeName(s string) string {
	b := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '_':
			b[i] = c
		default:
			b[i] = '_'
		}
	}
	return string(b)
}
