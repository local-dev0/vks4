// Пакет sipbridge — SIP peer transport для media-worker.
// SIP-клиенты (через FreeSWITCH B2BUA) не используют WebRTC/DTLS — только plain RTP.
// Открывает UDP listener'ы (audio + video), форвардит входящие RTP в pipeline-каналы,
// и шлёт audio обратно к sip-gateway (egress video в эту итерацию не входит).
package sipbridge

import (
	"net"
	"sync"

	"go.uber.org/zap"
)

type Peer struct {
	ID string

	AudioConn *net.UDPConn // listens audio RTP from sip-gateway; same socket used to send back
	AudioPort int
	VideoConn *net.UDPConn // listens video RTP from sip-gateway (одностороннее: ingress)
	VideoPort int

	mu              sync.RWMutex
	audioRemoteAddr *net.UDPAddr // sip-gateway audio endpoint (auto-learned)
	videoRemoteAddr *net.UDPAddr // sip-gateway video endpoint (auto-learned)

	audioIn chan<- []byte
	videoIn chan<- []byte
	stop    chan struct{}
	log     *zap.Logger
}

// NewPeer открывает два UDP listener'а: один для audio, второй для video.
// audioIn / videoIn — каналы pipeline (depay+decode там).
// videoIn может быть nil (тогда video listener не запускается).
func NewPeer(peerID string, audioIn, videoIn chan<- []byte, log *zap.Logger) (*Peer, error) {
	aconn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
	if err != nil {
		return nil, err
	}
	p := &Peer{
		ID:        peerID,
		AudioConn: aconn,
		AudioPort: aconn.LocalAddr().(*net.UDPAddr).Port,
		audioIn:   audioIn,
		videoIn:   videoIn,
		stop:      make(chan struct{}),
		log:       log,
	}
	if videoIn != nil {
		vconn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
		if err != nil {
			_ = aconn.Close()
			return nil, err
		}
		p.VideoConn = vconn
		p.VideoPort = vconn.LocalAddr().(*net.UDPAddr).Port
		go p.recvVideoLoop()
	}
	go p.recvAudioLoop()
	return p, nil
}

// Send отправляет RTP пакет (audio) к audio-remote endpoint. No-op если remote ещё не известен.
func (p *Peer) Send(pkt []byte) error {
	p.mu.RLock()
	addr := p.audioRemoteAddr
	p.mu.RUnlock()
	if addr == nil {
		return nil
	}
	_, err := p.AudioConn.WriteToUDP(pkt, addr)
	return err
}

// SendVideo отправляет RTP пакет (H.264) к video-remote endpoint. No-op если video не открыт.
func (p *Peer) SendVideo(pkt []byte) error {
	if p.VideoConn == nil {
		return nil
	}
	p.mu.RLock()
	addr := p.videoRemoteAddr
	p.mu.RUnlock()
	if addr == nil {
		return nil
	}
	_, err := p.VideoConn.WriteToUDP(pkt, addr)
	return err
}

func (p *Peer) recvAudioLoop() {
	buf := make([]byte, 1500)
	var pkts uint64
	for {
		select {
		case <-p.stop:
			return
		default:
		}
		n, addr, err := p.AudioConn.ReadFromUDP(buf)
		if err != nil {
			p.log.Debug("sip audio rtp recv error", zap.String("peer", p.ID), zap.Error(err))
			return
		}
		p.mu.Lock()
		if p.audioRemoteAddr == nil || !p.audioRemoteAddr.IP.Equal(addr.IP) || p.audioRemoteAddr.Port != addr.Port {
			p.audioRemoteAddr = addr // auto-learn
		}
		p.mu.Unlock()
		pkts++
		if pkts == 1 || pkts%500 == 0 {
			p.log.Info("sip rtp recv", zap.String("peer", p.ID), zap.Uint64("pkts", pkts), zap.Int("size", n))
		}
		// NAT-binding фильтр: 12-байтный RTP header без payload — нужен только для auto-learn,
		// в pipeline/SFU не нужен (ломает jitter buffer из-за рандомного SSRC).
		if n <= 12 {
			continue
		}
		pkt := make([]byte, n)
		copy(pkt, buf[:n])
		select {
		case p.audioIn <- pkt:
		default:
		}
	}
}

func (p *Peer) recvVideoLoop() {
	buf := make([]byte, 1500)
	var pkts uint64
	for {
		select {
		case <-p.stop:
			return
		default:
		}
		n, addr, err := p.VideoConn.ReadFromUDP(buf)
		if err != nil {
			p.log.Debug("sip video rtp recv error", zap.String("peer", p.ID), zap.Error(err))
			return
		}
		p.mu.Lock()
		if p.videoRemoteAddr == nil || !p.videoRemoteAddr.IP.Equal(addr.IP) || p.videoRemoteAddr.Port != addr.Port {
			p.videoRemoteAddr = addr // auto-learn sip-gateway video source addr
		}
		p.mu.Unlock()
		pkts++
		if pkts == 1 || pkts%500 == 0 {
			p.log.Info("sip video rtp recv", zap.String("peer", p.ID), zap.Uint64("pkts", pkts), zap.Int("size", n))
		}
		if n <= 12 {
			continue
		}
		pkt := make([]byte, n)
		copy(pkt, buf[:n])
		select {
		case p.videoIn <- pkt:
		default:
		}
	}
}

func (p *Peer) Close() {
	select {
	case <-p.stop:
	default:
		close(p.stop)
	}
	_ = p.AudioConn.Close()
	if p.VideoConn != nil {
		_ = p.VideoConn.Close()
	}
}
