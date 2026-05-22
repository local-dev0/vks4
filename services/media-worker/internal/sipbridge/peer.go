// Пакет sipbridge — SIP peer transport для media-worker.
// SIP-клиенты (через FreeSWITCH B2BUA) не используют WebRTC/DTLS — только plain RTP.
// Этот модуль открывает UDP listener, форвардит входящие пакеты в pipeline.AddPeer audio
// channel, и шлёт исходящие пакеты (MCU output) обратно к sip-gateway.
package sipbridge

import (
	"net"
	"sync"

	"go.uber.org/zap"
)

type Peer struct {
	ID        string
	AudioConn *net.UDPConn // listens RTP from sip-gateway; same socket used to send back
	AudioPort int

	mu         sync.RWMutex
	remoteAddr *net.UDPAddr // sip-gateway endpoint (auto-learned по first inbound packet)

	audioIn chan<- []byte
	stop    chan struct{}
	log     *zap.Logger
}

// NewPeer открывает UDP listener на любом свободном порту. audioIn — pipeline audio channel.
func NewPeer(peerID string, audioIn chan<- []byte, log *zap.Logger) (*Peer, error) {
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
	if err != nil {
		return nil, err
	}
	p := &Peer{
		ID:        peerID,
		AudioConn: conn,
		AudioPort: conn.LocalAddr().(*net.UDPAddr).Port,
		audioIn:   audioIn,
		stop:      make(chan struct{}),
		log:       log,
	}
	go p.recvLoop()
	return p, nil
}

// SetRemote явно указывает endpoint sip-gateway. Если не вызвать, peer learn'ит по первому
// принятому пакету. Полезно если нужно начать слать audio к peer'у до того как он что-то прислал.
func (p *Peer) SetRemote(addr *net.UDPAddr) {
	p.mu.Lock()
	p.remoteAddr = addr
	p.mu.Unlock()
}

// Send отправляет RTP пакет (Opus) к remote endpoint. No-op если remote ещё не известен.
func (p *Peer) Send(pkt []byte) error {
	p.mu.RLock()
	addr := p.remoteAddr
	p.mu.RUnlock()
	if addr == nil {
		return nil
	}
	_, err := p.AudioConn.WriteToUDP(pkt, addr)
	return err
}

func (p *Peer) recvLoop() {
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
			p.log.Debug("sip rtp recv error", zap.String("peer", p.ID), zap.Error(err))
			return
		}
		p.mu.Lock()
		if p.remoteAddr == nil || !p.remoteAddr.IP.Equal(addr.IP) || p.remoteAddr.Port != addr.Port {
			p.remoteAddr = addr // auto-learn
		}
		p.mu.Unlock()
		pkts++
		if pkts == 1 || pkts%500 == 0 {
			p.log.Info("sip rtp recv", zap.String("peer", p.ID), zap.Uint64("pkts", pkts), zap.Int("size", n))
		}
		pkt := make([]byte, n)
		copy(pkt, buf[:n])
		select {
		case p.audioIn <- pkt:
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
}
