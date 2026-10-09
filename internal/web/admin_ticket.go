package web

import (
	"net/http"
	"strings"

	"github.com/Comune-di-Montesilvano/CruscottoPA/internal/database"
)

type adminTicketView struct {
	Enabled, Mock bool
	Queue, Base   string // Base: URL dell'interfaccia agenti di OTRS ("" = niente link)
	Tickets       []database.TicketSent
}

// otrsAgentBase: da …/otrs/nph-genericinterface.pl/Webservice/X a …/otrs/.
func otrsAgentBase(wsURL string) string {
	i := strings.Index(wsURL, "nph-genericinterface.pl")
	if i < 0 || !strings.HasPrefix(wsURL, "https://") {
		return ""
	}
	return wsURL[:i]
}

func (s *Server) handleAdminTickets(w http.ResponseWriter, r *http.Request) {
	list, err := s.db.ListTickets(200)
	if err != nil {
		s.serverError(w, err)
		return
	}
	s.renderPage(w, r, "admin_ticket.html", "ticket", adminTicketView{
		Enabled: s.ticketsEnabled(), Mock: s.cfg.OTRS.Mock(), Queue: s.cfg.OTRS.Queue,
		Base: otrsAgentBase(s.cfg.OTRS.URL), Tickets: list,
	})
}
