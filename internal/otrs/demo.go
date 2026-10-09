package otrs

import (
	"fmt"
	"strings"
	"time"
)

// NewDemoMock: OTRS finto per lo sviluppo (OTRS_URL=mock). Al primo accesso di
// un utente crea alcuni ticket di esempio, con stati diversi e risposte
// dell'assistenza, così widget, elenco e conversazione si vedono come in uso.
func NewDemoMock() *Mock {
	m := NewMock()
	m.demo = map[string]bool{}
	return m
}

// seedDemo (con il lock): ticket di esempio per email, una sola volta.
func (m *Mock) seedDemo(email string) {
	key := strings.ToLower(email)
	if m.demo == nil || m.demo[key] {
		return
	}
	m.demo[key] = true
	now := time.Now()
	ago := func(h int) time.Time { return now.Add(-time.Duration(h) * time.Hour) }
	add := func(title, state string, created time.Time, arts ...Article) {
		m.next++
		id := fmt.Sprintf("9%03d", m.next)
		t := &Ticket{Summary: Summary{TicketID: id, TicketNumber: "20261001000" + id, Title: title, State: state, StateType: state,
			Created: created, Changed: created, Closed: state == "closed"}, CustomerUserID: email}
		for i, a := range arts {
			a.ArticleID = fmt.Sprintf("%s%02d", id, i+1)
			if a.Created.After(t.Changed) {
				t.Changed = a.Created
			}
			if a.FromAgent && a.Created.After(t.LastAgentArticle) {
				t.LastAgentArticle = a.Created
			}
			t.Articles = append(t.Articles, a)
		}
		m.Tickets[id] = t
	}
	add("La stampante del secondo piano non stampa", "open", ago(50),
		Article{From: "Tu", Body: "Da stamattina la stampante del secondo piano dà errore di carta inceppata, ma non c'è carta inceppata.", Created: ago(50)},
		Article{FromAgent: true, From: "Assistenza", Body: "Buongiorno, passiamo nel pomeriggio a controllare il cassetto 2. Nel frattempo può usare la stampante del primo piano.", Created: ago(26)},
		Article{From: "Tu", Body: "Grazie, intanto uso quella del primo piano.", Created: ago(25)},
		Article{FromAgent: true, From: "Assistenza", Body: "Abbiamo sostituito il rullo di trascinamento. Ci conferma che ora funziona?", Created: ago(2)})
	add("Richiesta accesso alla cartella condivisa del protocollo", "new", ago(5),
		Article{From: "Tu", Body: "Avrei bisogno dell'accesso in lettura alla cartella condivisa del protocollo.", Created: ago(5)})
	add("Rinnovo della password di posta", "pending reminder", ago(80),
		Article{From: "Tu", Body: "La password di posta mi scade domani: come la rinnovo?", Created: ago(80)},
		Article{FromAgent: true, From: "Assistenza", Body: "Le abbiamo inviato le istruzioni. Restiamo in attesa di una sua conferma.", Created: ago(70)})
	add("Monitor che sfarfalla", "closed", ago(120),
		Article{From: "Tu", Body: "Il monitor sfarfalla ogni tanto.", Created: ago(120)},
		Article{FromAgent: true, From: "Assistenza", Body: "Sostituito il cavo video. Chiudiamo il ticket: se si ripresenta, risponda pure qui.", Created: ago(96)})
}
