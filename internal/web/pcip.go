package web

import (
	"context"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// IP del PC: quello della connessione (X-Real-Ip del reverse proxy);
// altrimenti il nome della workstation (NTLM, dichiarato) più il
// suffisso del dominio, risolto nel DNS di Active Directory, dove i PC si
// registrano da soli. Solo un'etichetta per l'assistenza (VNC), mai per autorizzare.
const (
	pcIPTTL        = 10 * time.Minute
	pcIPMissTTL    = time.Minute
	pcLookupTimout = 2 * time.Second
)

type cachedIP struct {
	ip      string
	expires time.Time
}

type pcIPCache struct {
	mu sync.Mutex
	m  map[string]cachedIP
}

// pcAddr: IP del PC da mostrare (connessione, altrimenti DNS) e quello del DNS,
// per segnalare all'assistenza quando non coincidono.
type pcAddr struct{ IP, DNS string }

// Mismatch: connessione e DNS indicano IP diversi (VPN, cambio di rete, record vecchio).
func (a pcAddr) Mismatch() bool { return a.IP != "" && a.DNS != "" && a.IP != a.DNS }

// pcAddress: IP della connessione se c'è, altrimenti il DNS.
func (s *Server) pcAddress(r *http.Request, pc string) pcAddr {
	a := pcAddr{DNS: s.pcIP(pc)}
	a.IP = s.realIP(r)
	if a.IP == "" {
		a.IP = a.DNS
	}
	return a
}

// realIP: X-Real-Ip messo dal reverse proxy (RemoteAddr è la rete di Podman).
// Header accettato così com'è: l'IP è un'etichetta per l'assistenza, come il
// nome NTLM. Solo IPv4 (serve a VNC); "" se assente o non valido.
func (s *Server) realIP(r *http.Request) string {
	p := net.ParseIP(strings.TrimSpace(r.Header.Get("X-Real-Ip")))
	if p == nil || p.To4() == nil {
		return ""
	}
	return p.String()
}

// newPCLookup: resolver Go; con server il DNS interrogato è sempre quello
// (il DNS del container può non conoscere i nomi del dominio).
func newPCLookup(server string) func(ctx context.Context, host string) ([]string, error) {
	r := &net.Resolver{PreferGo: true}
	if server != "" {
		r.Dial = func(ctx context.Context, network, _ string) (net.Conn, error) {
			d := net.Dialer{Timeout: pcLookupTimout}
			return d.DialContext(ctx, network, server)
		}
	}
	return r.LookupHost
}

// pcIP: primo indirizzo IPv4 del PC, "" se spento (PC_DNS_SUFFIX vuoto), senza
// nome o non risolto. In cache 10 minuti (1 se non trovato).
func (s *Server) pcIP(pc string) string {
	if pc == "" || s.cfg.PCDNSSuffix == "" || s.pcLookup == nil {
		return ""
	}
	host := strings.ToLower(pc + "." + s.cfg.PCDNSSuffix)
	now := s.now()
	s.pcIPs.mu.Lock()
	e, ok := s.pcIPs.m[host]
	s.pcIPs.mu.Unlock()
	if ok && now.Before(e.expires) {
		return e.ip
	}
	ctx, cancel := context.WithTimeout(context.Background(), pcLookupTimout)
	defer cancel()
	ip := ""
	if addrs, err := s.pcLookup(ctx, host); err == nil {
		for _, a := range addrs {
			if p := net.ParseIP(a); p != nil && p.To4() != nil {
				ip = p.String()
				break
			}
		}
	}
	ttl := pcIPTTL
	if ip == "" {
		ttl = pcIPMissTTL
	}
	s.pcIPs.mu.Lock()
	if len(s.pcIPs.m) > 5000 { // migliaia di PC: oltre si riparte da zero
		s.pcIPs.m = map[string]cachedIP{}
	}
	s.pcIPs.m[host] = cachedIP{ip: ip, expires: now.Add(ttl)}
	s.pcIPs.mu.Unlock()
	return ip
}
