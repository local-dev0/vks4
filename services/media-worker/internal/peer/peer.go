// Peer — обёртка вокруг Pion PeerConnection.
package peer

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/pion/interceptor"
	"github.com/pion/rtcp"
	"github.com/pion/webrtc/v4"
	"go.uber.org/zap"
)

type Sink interface {
	OnVideoRTP(peerID string, pkt []byte)
	OnAudioRTP(peerID string, pkt []byte)
}

type Peer struct {
	ID     string
	RoomID string

	pc       *webrtc.PeerConnection
	cfg      webrtc.Configuration
	log      *zap.Logger
	sink     Sink

	// Pre-allocated outgoing slots. Slot N принадлежит peer'у с slot=N в комнате,
	// и в этот трек идут все RTP-пакеты от того peer'а.
	videoSlots []*webrtc.TrackLocalStaticRTP
	audioSlots []*webrtc.TrackLocalStaticRTP

	mu     sync.Mutex
	closed bool
}

// VideoSlotCount — количество video-дорожек на каждом peer.
// Для MCU нужен только один: slot-0 содержит микшированный кадр сервера.
const VideoSlotCount = 1

// AudioSlotCount — количество audio-дорожек на каждом peer (для SFU-форварда / mix-minus).
// Поддерживаем до 20 одновременных аудио-источников (peer A→slot[A].audio у всех других).
const AudioSlotCount = 20

// SlotCount — общий лимит slots в комнате (равен AudioSlotCount, т.к. forwardAudio использует
// slot отправителя как индекс в peer.audioSlots у получателей).
const SlotCount = AudioSlotCount

type Options struct {
	ICE        []webrtc.ICEServer
	Log        *zap.Logger
	Sink       Sink
	VideoCodec string // mime: video/VP8 | video/H264
	PublicIP   string
	UDPMin     uint16
	UDPMax     uint16
}

func New(roomID, peerID string, opts Options) (*Peer, error) {
	mediaEngine := &webrtc.MediaEngine{}
	if err := mediaEngine.RegisterDefaultCodecs(); err != nil {
		return nil, fmt.Errorf("register codecs: %w", err)
	}
	registry := &interceptor.Registry{}
	if err := webrtc.RegisterDefaultInterceptors(mediaEngine, registry); err != nil {
		return nil, fmt.Errorf("interceptors: %w", err)
	}
	settings := webrtc.SettingEngine{}
	if opts.UDPMin > 0 && opts.UDPMax >= opts.UDPMin {
		if err := settings.SetEphemeralUDPPortRange(opts.UDPMin, opts.UDPMax); err != nil {
			return nil, fmt.Errorf("udp range: %w", err)
		}
	}
	if opts.PublicIP != "" {
		settings.SetNAT1To1IPs([]string{opts.PublicIP}, webrtc.ICECandidateTypeHost)
	}
	api := webrtc.NewAPI(
		webrtc.WithMediaEngine(mediaEngine),
		webrtc.WithInterceptorRegistry(registry),
		webrtc.WithSettingEngine(settings),
	)
	pc, err := api.NewPeerConnection(webrtc.Configuration{ICEServers: opts.ICE})
	if err != nil {
		return nil, fmt.Errorf("new pc: %w", err)
	}
	codec := opts.VideoCodec
	if codec == "" {
		codec = webrtc.MimeTypeVP8
	}

	// 1) recv-only transceivers для приёма видео и аудио от клиента (1 video + 1 audio).
	if _, err := pc.AddTransceiverFromKind(webrtc.RTPCodecTypeVideo, webrtc.RTPTransceiverInit{
		Direction: webrtc.RTPTransceiverDirectionRecvonly,
	}); err != nil {
		return nil, fmt.Errorf("add recv video transceiver: %w", err)
	}
	if _, err := pc.AddTransceiverFromKind(webrtc.RTPCodecTypeAudio, webrtc.RTPTransceiverInit{
		Direction: webrtc.RTPTransceiverDirectionRecvonly,
	}); err != nil {
		return nil, fmt.Errorf("add recv audio transceiver: %w", err)
	}

	// 2) Sendonly outputs:
	//    - 1 video track (slot-0) — MCU output (микшированный сервером кадр всех peer-ов).
	//    - 20 audio tracks (slot-0..slot-19) — SFU forward, peer A's audio попадает в slot[A].audio
	//      у других peer-ов (mix-minus без затрат на N×N audiomixer).
	videoSlots := make([]*webrtc.TrackLocalStaticRTP, 0, VideoSlotCount)
	for i := 0; i < VideoSlotCount; i++ {
		vTrack, err := webrtc.NewTrackLocalStaticRTP(
			webrtc.RTPCodecCapability{MimeType: codec},
			fmt.Sprintf("video-%d", i),
			fmt.Sprintf("slot-%d", i),
		)
		if err != nil {
			return nil, fmt.Errorf("video slot %d: %w", i, err)
		}
		if _, err := pc.AddTrack(vTrack); err != nil {
			return nil, fmt.Errorf("AddTrack video slot %d: %w", i, err)
		}
		videoSlots = append(videoSlots, vTrack)
	}
	audioSlots := make([]*webrtc.TrackLocalStaticRTP, 0, AudioSlotCount)
	for i := 0; i < AudioSlotCount; i++ {
		aTrack, err := webrtc.NewTrackLocalStaticRTP(
			webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus},
			fmt.Sprintf("audio-%d", i),
			fmt.Sprintf("aslot-%d", i),
		)
		if err != nil {
			return nil, fmt.Errorf("audio slot %d: %w", i, err)
		}
		if _, err := pc.AddTrack(aTrack); err != nil {
			return nil, fmt.Errorf("AddTrack audio slot %d: %w", i, err)
		}
		audioSlots = append(audioSlots, aTrack)
	}

	p := &Peer{
		ID: peerID, RoomID: roomID, pc: pc, log: opts.Log, sink: opts.Sink,
		videoSlots: videoSlots, audioSlots: audioSlots,
	}
	pc.OnTrack(p.onTrack)
	pc.OnICEConnectionStateChange(func(s webrtc.ICEConnectionState) {
		opts.Log.Debug("ice state", zap.String("peer", peerID), zap.String("state", s.String()))
	})
	pc.OnConnectionStateChange(func(s webrtc.PeerConnectionState) {
		opts.Log.Info("pc state", zap.String("peer", peerID), zap.String("state", s.String()))
		if s == webrtc.PeerConnectionStateFailed || s == webrtc.PeerConnectionStateClosed {
			_ = p.Close()
		}
	})
	return p, nil
}

func (p *Peer) onTrack(track *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
	p.log.Info("ontrack",
		zap.String("peer", p.ID),
		zap.String("kind", track.Kind().String()),
		zap.String("codec", track.Codec().MimeType),
		zap.Uint8("pt", uint8(track.PayloadType())),
	)
	// Для video регулярно отправляем PLI (Picture Loss Indication) — это заставит
	// клиент сразу выслать keyframe. Без этого декодер на сервере вечно ждёт I-frame
	// и compositor получает чёрную картинку.
	if track.Kind() == webrtc.RTPCodecTypeVideo {
		ssrc := uint32(track.SSRC())
		go func() {
			// Сразу шлём PLI, чтобы клиент быстро прислал keyframe.
			_ = p.pc.WriteRTCP([]rtcp.Packet{&rtcp.PictureLossIndication{MediaSSRC: ssrc}})
			// Затем каждую секунду — для восстановления после потерь и снижения зависаний.
			ticker := time.NewTicker(1 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					if err := p.pc.WriteRTCP([]rtcp.Packet{&rtcp.PictureLossIndication{MediaSSRC: ssrc}}); err != nil {
						return
					}
				}
			}
		}()
	}
	go func() {
		buf := make([]byte, 1500)
		isVideo := track.Kind() == webrtc.RTPCodecTypeVideo
		var pkts uint64
		for {
			n, _, err := track.Read(buf)
			if err != nil {
				if err != io.EOF {
					p.log.Info("track read end", zap.String("peer", p.ID), zap.Error(err), zap.Uint64("pkts", pkts))
				}
				return
			}
			pkts++
			if pkts == 1 {
				p.log.Info("track read first packet",
					zap.String("peer", p.ID),
					zap.Bool("video", isVideo),
					zap.Int("n", n))
			}
			pkt := make([]byte, n)
			copy(pkt, buf[:n])
			if isVideo {
				if p.sink != nil {
					p.sink.OnVideoRTP(p.ID, pkt)
				}
			} else {
				if p.sink != nil {
					p.sink.OnAudioRTP(p.ID, pkt)
				}
			}
		}
	}()
}

// Negotiate выполняет SDP-handshake: принимает offer от клиента, возвращает answer.
func (p *Peer) Negotiate(ctx context.Context, offerSDP string) (string, error) {
	offer := webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: offerSDP}
	if err := p.pc.SetRemoteDescription(offer); err != nil {
		return "", fmt.Errorf("set remote: %w", err)
	}
	gatherDone := webrtc.GatheringCompletePromise(p.pc)
	answer, err := p.pc.CreateAnswer(nil)
	if err != nil {
		return "", fmt.Errorf("create answer: %w", err)
	}
	if err := p.pc.SetLocalDescription(answer); err != nil {
		return "", fmt.Errorf("set local: %w", err)
	}
	<-gatherDone
	return p.pc.LocalDescription().SDP, nil
}

// WriteVideoSlot пушит RTP-пакет в нужный slot (= позиция peer-источника в комнате).
func (p *Peer) WriteVideoSlot(slot int, pkt []byte) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed || slot < 0 || slot >= len(p.videoSlots) {
		return nil
	}
	_, err := p.videoSlots[slot].Write(pkt)
	return err
}

func (p *Peer) WriteAudioSlot(slot int, pkt []byte) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed || slot < 0 || slot >= len(p.audioSlots) {
		return nil
	}
	_, err := p.audioSlots[slot].Write(pkt)
	return err
}

func (p *Peer) Close() error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil
	}
	p.closed = true
	p.mu.Unlock()
	return p.pc.Close()
}
