package web

import (
	"sync"
	"time"
)

// Limiti della chat per mittente, in memoria: 30 messaggi al minuto, 300
// all'ora, 20 conversazioni nuove al giorno (frena lo spam a tappeto).
const (
	chatPerMinute     = 30
	chatPerHour       = 300
	chatNewPerDay     = 20
	chatLimitsMaxKeys = 5000
)

type chatLimits struct {
	mu   sync.Mutex
	sent map[string][]time.Time // invii dell'ultima ora
	news map[string]chatDayCount
}

type chatDayCount struct {
	day string
	n   int
}

func newChatLimits() *chatLimits {
	return &chatLimits{sent: map[string][]time.Time{}, news: map[string]chatDayCount{}}
}

// allowMessage conta l'invio se rientra nei limiti.
func (l *chatLimits) allowMessage(user string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.sent) > chatLimitsMaxKeys {
		for k, ts := range l.sent {
			if len(ts) == 0 || now.Sub(ts[len(ts)-1]) > time.Hour {
				delete(l.sent, k)
			}
		}
	}
	keep := l.sent[user][:0]
	minute := 0
	for _, t := range l.sent[user] {
		if now.Sub(t) < time.Hour {
			keep = append(keep, t)
			if now.Sub(t) < time.Minute {
				minute++
			}
		}
	}
	if len(keep) >= chatPerHour || minute >= chatPerMinute {
		l.sent[user] = keep
		return false
	}
	l.sent[user] = append(keep, now)
	return true
}

// allowNewConversation conta una conversazione nuova nel giorno day (Roma).
func (l *chatLimits) allowNewConversation(user, day string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.news) > chatLimitsMaxKeys {
		for k, c := range l.news {
			if c.day != day {
				delete(l.news, k)
			}
		}
	}
	c := l.news[user]
	if c.day != day {
		c = chatDayCount{day: day}
	}
	if c.n >= chatNewPerDay {
		return false
	}
	c.n++
	l.news[user] = c
	return true
}
