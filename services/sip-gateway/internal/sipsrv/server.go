// Пакет sipsrv — SIP сервер на базе emiago/sipgo. Принимает INVITE от FreeSWITCH (B2BUA leg),
// открывает RTP listener на свободном UDP-порту, отвечает 200 OK со собственным SDP.
// Реальный forward в media-worker — следующая итерация.
package sipsrv

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/emiago/sipgo"
	"github.com/emiago/sipgo/sip"
	"go.uber.org/zap"

	"github.com/vks4/vks4/services/sip-gateway/internal/bridge"
)

var httpClient = &http.Client{Timeout: 5 * time.Second}

func jsonMarshal(v any) (io.Reader, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return bytes.NewReader(b), nil
}

func jsonDecode(r io.Reader, v any) error { return json.NewDecoder(r).Decode(v) }

type Config struct {
	BindAddr        string // ":5070"
	PublicIP        string // адрес для SDP c-line (то что пишем в Contact/SDP)
	RTPMinPort      uint16
	RTPMaxPort      uint16
	MediaWorkerURL  string // http://media-worker:9091
	MediaWorkerHost string // media-worker (для RTP target IP)
	FSHost          string // freeswitch (используется для подсказки auto-detect SDP IP)
}

type Server struct {
	cfg    Config
	log    *zap.Logger
	br     *bridge.Bridge
	ua     *sipgo.UserAgent
	server *sipgo.Server

	mu    sync.Mutex
	calls map[string]*Call // call-id → Call (only audio supported)
}

type Call struct {
	CallID      string
	Room        string
	Caller      string
	PeerID      string       // peerID в media-worker
	FSConn      *net.UDPConn // ← / → FreeSWITCH (sip-gateway's local RTP, в SDP answer)
	FSRemote    *net.UDPAddr // FS RTP endpoint (auto-learn)
	MWConn      *net.UDPConn // ← / → media-worker (отдельный UDP socket)
	MWRemote    *net.UDPAddr // media-worker RTP endpoint
	stop        chan struct{}
}

func New(cfg Config, log *zap.Logger, br *bridge.Bridge) (*Server, error) {
	if cfg.BindAddr == "" {
		cfg.BindAddr = ":5070"
	}
	if cfg.PublicIP == "" {
		// Auto-detect: предпочитаем интерфейс в той же подсети, что и FreeSWITCH.
		// У контейнера sip-gateway несколько docker-сетей (app, media); SIP-сигнализация
		// и RTP с FS идут по одной из них — выбираем именно её.
		ip, err := detectLocalIP(cfg.FSHost)
		if err != nil {
			return nil, fmt.Errorf("cannot detect local IP for SDP: %w", err)
		}
		cfg.PublicIP = ip
		log.Info("auto-detected local IP for SDP",
			zap.String("ip", cfg.PublicIP),
			zap.String("fs-host", cfg.FSHost),
		)
	}
	if cfg.RTPMinPort == 0 {
		cfg.RTPMinPort = 17000
	}
	if cfg.RTPMaxPort == 0 {
		cfg.RTPMaxPort = 17200
	}
	ua, err := sipgo.NewUA(sipgo.WithUserAgent("vks4-sipgw"))
	if err != nil {
		return nil, fmt.Errorf("new ua: %w", err)
	}
	srv, err := sipgo.NewServer(ua)
	if err != nil {
		return nil, fmt.Errorf("new server: %w", err)
	}
	s := &Server{cfg: cfg, log: log, br: br, ua: ua, server: srv, calls: map[string]*Call{}}
	srv.OnInvite(s.onInvite)
	srv.OnAck(s.onAck)
	srv.OnBye(s.onBye)
	srv.OnCancel(s.onCancel)
	return s, nil
}

// Start запускает SIP listener (UDP по умолчанию). Блокирующий — запускать в goroutine.
func (s *Server) Start(ctx context.Context) error {
	host, port, err := splitHostPort(s.cfg.BindAddr)
	if err != nil {
		return err
	}
	s.log.Info("sip server listening", zap.String("transport", "udp"), zap.String("bind", s.cfg.BindAddr))
	return s.server.ListenAndServe(ctx, "udp", net.JoinHostPort(host, port))
}

func splitHostPort(addr string) (string, string, error) {
	if !strings.Contains(addr, ":") {
		return "0.0.0.0", addr, nil
	}
	h, p, err := net.SplitHostPort(addr)
	if err != nil {
		return "", "", err
	}
	if h == "" {
		h = "0.0.0.0"
	}
	return h, p, nil
}

// onInvite принимает входящий звонок от FreeSWITCH. Стратегия:
// 1) Парсим INVITE SDP — извлекаем remote RTP endpoint.
// 2) Открываем локальный UDP listener для приёма RTP (sip-gateway side).
// 3) Формируем own SDP с PublicIP + локальным портом.
// 4) Отвечаем 200 OK.
// Media-bridge в media-worker — следующая итерация. Сейчас просто принимаем RTP и log'аем счётчик.
func (s *Server) onInvite(req *sip.Request, tx sip.ServerTransaction) {
	callID := req.CallID().Value()
	room := getHeaderValue(req, "X-VKS-Room")
	caller := getHeaderValue(req, "X-VKS-Caller")
	if caller == "" {
		if f := req.From(); f != nil {
			caller = f.Address.User
		}
	}

	remoteIP, remotePort, ok := parseSDPRTP(string(req.Body()))
	if !ok {
		s.log.Warn("invite: cannot parse SDP", zap.String("call-id", callID))
		_ = tx.Respond(sip.NewResponseFromRequest(req, 488, "Not Acceptable Here", nil))
		return
	}

	// FSConn: UDP listener для приёма RTP от FS (используется в SDP answer как local port).
	fsConn, localPort, err := openRTPListener(s.cfg.RTPMinPort, s.cfg.RTPMaxPort)
	if err != nil {
		s.log.Error("rtp listener", zap.Error(err))
		_ = tx.Respond(sip.NewResponseFromRequest(req, 500, "Internal Error", nil))
		return
	}
	fsRemote := &net.UDPAddr{IP: net.ParseIP(remoteIP), Port: remotePort}

	// Создаём peer в media-worker. Возвращает UDP port куда мы будем форвардить RTP.
	peerID := callID // одна-к-одной mapping
	mwPort, err := s.createMWPeer(peerID, caller, room)
	if err != nil {
		s.log.Error("create mw peer", zap.Error(err))
		_ = fsConn.Close()
		_ = tx.Respond(sip.NewResponseFromRequest(req, 503, "Service Unavailable", nil))
		return
	}
	mwAddr := &net.UDPAddr{IP: net.ParseIP(resolveOnce(s.cfg.MediaWorkerHost)), Port: mwPort}
	// MWConn: отдельный UDP socket для общения с media-worker.
	mwConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
	if err != nil {
		s.log.Error("mw udp dial", zap.Error(err))
		_ = fsConn.Close()
		_ = tx.Respond(sip.NewResponseFromRequest(req, 500, "Internal Error", nil))
		return
	}

	call := &Call{
		CallID: callID, Room: room, Caller: caller, PeerID: peerID,
		FSConn: fsConn, FSRemote: fsRemote,
		MWConn: mwConn, MWRemote: mwAddr,
		stop: make(chan struct{}),
	}
	s.mu.Lock()
	s.calls[callID] = call
	s.mu.Unlock()
	s.log.Info("SIP INVITE accepted",
		zap.String("call-id", callID),
		zap.String("caller", caller),
		zap.String("room", room),
		zap.String("fs-remote", fmt.Sprintf("%s:%d", remoteIP, remotePort)),
		zap.Uint16("local-rtp", localPort),
		zap.String("mw-remote", mwAddr.String()),
	)
	s.br.Accept(callID, room, caller)

	go s.relayFStoMW(call)
	go s.relayMWtoFS(call)
	// Проактивный NAT-binding: сразу шлём пустые RTP-пакеты в обе стороны.
	// FS с rtp-auto-adjust=true (default) тогда зафиксирует наш фактический source IP
	// и будет слать обратные RTP туда — независимо от того что мы указали в SDP.
	// Media-worker через auto-learn получит наш MWConn source — он начнёт слать MCU output.
	go s.natBindLoop(call)

	// Формируем SDP answer. Поддерживаем только Opus (FS делает transcoding из любых кодеков).
	answer := buildSDPAnswer(s.cfg.PublicIP, localPort)

	res := sip.NewResponseFromRequest(req, 200, "OK", []byte(answer))
	ct := sip.NewHeader("Content-Type", "application/sdp")
	res.AppendHeader(ct)
	if err := tx.Respond(res); err != nil {
		s.log.Error("respond 200", zap.Error(err))
	}
}

func (s *Server) onAck(req *sip.Request, tx sip.ServerTransaction) {
	callID := req.CallID().Value()
	s.log.Debug("SIP ACK", zap.String("call-id", callID))
}

func (s *Server) onBye(req *sip.Request, tx sip.ServerTransaction) {
	callID := req.CallID().Value()
	s.cleanup(callID)
	res := sip.NewResponseFromRequest(req, 200, "OK", nil)
	_ = tx.Respond(res)
	s.log.Info("SIP BYE", zap.String("call-id", callID))
}

func (s *Server) onCancel(req *sip.Request, tx sip.ServerTransaction) {
	callID := req.CallID().Value()
	s.cleanup(callID)
	res := sip.NewResponseFromRequest(req, 200, "OK", nil)
	_ = tx.Respond(res)
	s.log.Info("SIP CANCEL", zap.String("call-id", callID))
}

func (s *Server) cleanup(callID string) {
	s.mu.Lock()
	call := s.calls[callID]
	delete(s.calls, callID)
	s.mu.Unlock()
	if call == nil {
		return
	}
	close(call.stop)
	if call.FSConn != nil {
		_ = call.FSConn.Close()
	}
	if call.MWConn != nil {
		_ = call.MWConn.Close()
	}
	// Уведомляем media-worker о завершении peer'а.
	if call.PeerID != "" && call.Room != "" {
		go func(peer, room string) {
			url := fmt.Sprintf("%s/rooms/%s/sip-peers/%s", s.cfg.MediaWorkerURL, room, peer)
			req, _ := http.NewRequest(http.MethodDelete, url, nil)
			_, _ = httpClient.Do(req)
		}(call.PeerID, call.Room)
	}
	s.br.Release(callID)
}

// relayFStoMW: FreeSWITCH RTP → media-worker. Auto-learn FSRemote по первому inbound пакету.
func (s *Server) relayFStoMW(call *Call) {
	buf := make([]byte, 1500)
	var count uint64
	for {
		select {
		case <-call.stop:
			return
		default:
		}
		n, addr, err := call.FSConn.ReadFromUDP(buf)
		if err != nil {
			return
		}
		if call.FSRemote == nil || !call.FSRemote.IP.Equal(addr.IP) || call.FSRemote.Port != addr.Port {
			call.FSRemote = addr
		}
		count++
		if count == 1 || count%500 == 0 {
			s.log.Info("FS→MW relay",
				zap.String("call-id", call.CallID),
				zap.Uint64("pkts", count),
				zap.String("mw", call.MWRemote.String()),
			)
		}
		if _, err := call.MWConn.WriteToUDP(buf[:n], call.MWRemote); err != nil {
			s.log.Debug("mw write", zap.Error(err))
		}
	}
}

// relayMWtoFS: media-worker MCU output (Opus RTP) → FreeSWITCH → SIP terminal.
func (s *Server) relayMWtoFS(call *Call) {
	buf := make([]byte, 1500)
	var count uint64
	for {
		select {
		case <-call.stop:
			return
		default:
		}
		n, _, err := call.MWConn.ReadFromUDP(buf)
		if err != nil {
			return
		}
		count++
		if count == 1 || count%500 == 0 {
			s.log.Info("MW→FS relay",
				zap.String("call-id", call.CallID),
				zap.Uint64("pkts", count),
			)
		}
		if call.FSRemote == nil {
			continue // ждём auto-learn по incoming
		}
		if _, err := call.FSConn.WriteToUDP(buf[:n], call.FSRemote); err != nil {
			s.log.Debug("fs write", zap.Error(err))
		}
	}
}

// natBindLoop посылает минимальный RTP-пакет (12-байтовый header, PT=96, без payload)
// в FS и в media-worker каждые 200 мс пока не пошёл реальный media-стрим. Это нужно для:
//   - FS rtp-auto-adjust: FS перепривяжет remote RTP к нашему фактическому source addr,
//     даже если в SDP мы указали неправильный IP (multi-network контейнер).
//   - media-worker auto-learn: его sipbridge.Peer узнает наш MWConn addr и начнёт слать
//     MCU output обратно.
// Цикл сам останавливается по call.stop (закрывается при BYE/CANCEL).
func (s *Server) natBindLoop(call *Call) {
	// Минимальный RTP header: V=2, P=0, X=0, CC=0, M=0, PT=111 (Opus), seq=0, ts=0, ssrc=random.
	hdr := []byte{
		0x80, 111, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00,
	}
	// SSRC рандомный, чтобы FS не объединил с реальным потоком.
	ssrc := rand.Uint32()
	hdr[8] = byte(ssrc >> 24)
	hdr[9] = byte(ssrc >> 16)
	hdr[10] = byte(ssrc >> 8)
	hdr[11] = byte(ssrc)

	tick := time.NewTicker(200 * time.Millisecond)
	defer tick.Stop()
	var seq uint16
	for {
		select {
		case <-call.stop:
			return
		case <-tick.C:
		}
		seq++
		hdr[2] = byte(seq >> 8)
		hdr[3] = byte(seq)
		if call.FSRemote != nil {
			_, _ = call.FSConn.WriteToUDP(hdr, call.FSRemote)
		}
		if call.MWRemote != nil {
			_, _ = call.MWConn.WriteToUDP(hdr, call.MWRemote)
		}
	}
}

// createMWPeer вызывает POST /rooms/{room}/sip-peers в media-worker.
// Возвращает audioPort на стороне media-worker (куда нам форвардить RTP).
func (s *Server) createMWPeer(peerID, displayName, room string) (int, error) {
	url := fmt.Sprintf("%s/rooms/%s/sip-peers", s.cfg.MediaWorkerURL, room)
	body, _ := jsonMarshal(map[string]string{"peerId": peerID, "displayName": displayName})
	resp, err := httpClient.Post(url, "application/json", body)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return 0, fmt.Errorf("mw status %d", resp.StatusCode)
	}
	var out struct {
		AudioPort int `json:"audioPort"`
	}
	if err := jsonDecode(resp.Body, &out); err != nil {
		return 0, err
	}
	if out.AudioPort == 0 {
		return 0, fmt.Errorf("mw returned audioPort=0")
	}
	return out.AudioPort, nil
}

func resolveOnce(host string) string {
	if ip := net.ParseIP(host); ip != nil {
		return host
	}
	addrs, err := net.LookupHost(host)
	if err != nil || len(addrs) == 0 {
		return host
	}
	return addrs[0]
}

// detectLocalIP возвращает локальный IPv4. Если задан fsHost — резолвим его
// и выбираем интерфейс, чья подсеть содержит IP FreeSWITCH (т.е. та же docker network).
// Это критично: контейнер sip-gateway подключён к двум сетям (app, media), а FS — только
// к одной с точки зрения SIP+RTP-сигнализации. Если мы поставим IP из неправильной
// сети в SDP, FS может его получить, но обратные RTP-пакеты пойдут не туда.
func detectLocalIP(fsHost string) (string, error) {
	var fsIPs []net.IP
	if fsHost != "" {
		if addrs, err := net.LookupHost(fsHost); err == nil {
			for _, a := range addrs {
				if ip := net.ParseIP(a); ip != nil && ip.To4() != nil {
					fsIPs = append(fsIPs, ip.To4())
				}
			}
		}
	}
	ifaces, err := net.Interfaces()
	if err != nil {
		return "", err
	}
	var fallback string
	for _, iface := range ifaces {
		if iface.Flags&net.FlagLoopback != 0 || iface.Flags&net.FlagUp == 0 {
			continue
		}
		addrs, _ := iface.Addrs()
		for _, a := range addrs {
			ipnet, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			v4 := ipnet.IP.To4()
			if v4 == nil {
				continue
			}
			for _, fsIP := range fsIPs {
				if ipnet.Contains(fsIP) {
					return v4.String(), nil
				}
			}
			if fallback == "" {
				fallback = v4.String()
			}
		}
	}
	if fallback != "" {
		return fallback, nil
	}
	return "", errors.New("no usable interface")
}

// openRTPListener открывает UDP socket на любом свободном порту в [min,max].
// Возвращает conn + port (чтобы вставить в SDP answer).
func openRTPListener(minP, maxP uint16) (*net.UDPConn, uint16, error) {
	for tries := 0; tries < 50; tries++ {
		// Случайный порт в диапазоне для распределения нагрузки между call'ами.
		port := minP + uint16(rand.Intn(int(maxP-minP+1)))
		addr := &net.UDPAddr{IP: net.IPv4zero, Port: int(port)}
		conn, err := net.ListenUDP("udp", addr)
		if err == nil {
			return conn, port, nil
		}
	}
	return nil, 0, errors.New("no free RTP port")
}

// parseSDPRTP вытаскивает IP и port для audio RTP из SDP. Минимальный парсер для INVITE.
func parseSDPRTP(sdp string) (string, int, bool) {
	var globalIP string
	for _, line := range strings.Split(sdp, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "c=IN IP4 "):
			globalIP = strings.TrimPrefix(line, "c=IN IP4 ")
		case strings.HasPrefix(line, "m=audio "):
			parts := strings.Fields(strings.TrimPrefix(line, "m=audio "))
			if len(parts) >= 1 {
				var port int
				fmt.Sscanf(parts[0], "%d", &port)
				if port > 0 && globalIP != "" {
					return globalIP, port, true
				}
			}
		}
	}
	return "", 0, false
}

// buildSDPAnswer формирует минимальный SDP с Opus аудио на нашей стороне.
// PT=111 — Pion/media-worker convention для Opus (pipeline depayloader caps payload=111,
// outbound rtpopuspay pt=111). Если использовать 96, FS будет слать PT=96, и rtpopusdepay
// в media-worker pipeline отбросит пакеты — depayloader жёстко фильтрует по PT в caps.
func buildSDPAnswer(publicIP string, rtpPort uint16) string {
	return fmt.Sprintf(`v=0
o=vks4 %d %d IN IP4 %s
s=vks4-sipgw
c=IN IP4 %s
t=0 0
m=audio %d RTP/AVP 111
a=rtpmap:111 opus/48000/2
a=fmtp:111 useinbandfec=1
a=ptime:20
a=sendrecv
`, rand.Int31(), rand.Int31(), publicIP, publicIP, rtpPort)
}

func getHeaderValue(req *sip.Request, name string) string {
	for _, h := range req.Headers() {
		if strings.EqualFold(h.Name(), name) {
			return h.Value()
		}
	}
	return ""
}
