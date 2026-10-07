package identity

import (
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/go-ldap/ldap/v3"
)

// AttrStat descrive un attributo AD compilato sugli utenti, per aiutare a
// sceglierlo nella configurazione dei gruppi della plancia.
type AttrStat struct {
	Name     string
	Count    int      // utenti del campione con l'attributo compilato
	Examples []string // fino a 3 valori, i più frequenti
}

// Attributi tecnici o identificativi, inutili per raggruppare le persone.
var technicalAttrs = map[string]bool{
	"objectclass": true, "objectguid": true, "objectsid": true, "objectcategory": true,
	"distinguishedname": true, "cn": true, "name": true, "samaccountname": true,
	"userprincipalname": true, "samaccounttype": true, "useraccountcontrol": true,
	"primarygroupid": true, "memberof": true, "instancetype": true, "codepage": true,
	"countrycode": true, "logoncount": true, "badpwdcount": true, "badpasswordtime": true,
	"lastlogon": true, "lastlogontimestamp": true, "lastlogoff": true, "pwdlastset": true,
	"accountexpires": true, "whencreated": true, "whenchanged": true, "usncreated": true,
	"usnchanged": true, "dscorepropagationdata": true, "logonhours": true,
	"usercertificate": true, "thumbnailphoto": true, "jpegphoto": true, "mail": true,
	"proxyaddresses": true, "displayname": true, "givenname": true, "sn": true,
	"msds-supportedencryptiontypes": true, "lockouttime": true, "admincount": true,
}

// attrStats conta gli attributi compilati nelle voci; q filtra per nome.
// Ordine: più utenti per primi. Solo valori testuali brevi.
func attrStats(entries []*ldap.Entry, q string) []AttrStat {
	return filterStats(computeStats(entries), q)
}

// filterStats: attributi il cui nome contiene q (senza maiuscole), max 20.
func filterStats(all []AttrStat, q string) []AttrStat {
	q = strings.ToLower(strings.TrimSpace(q))
	out := []AttrStat{}
	for _, st := range all {
		if q == "" || strings.Contains(strings.ToLower(st.Name), q) {
			out = append(out, st)
			if len(out) == 20 {
				break
			}
		}
	}
	return out
}

func computeStats(entries []*ldap.Entry) []AttrStat {
	counts := map[string]int{}
	values := map[string]map[string]int{}
	names := map[string]string{} // minuscolo → nome come restituito da AD
	for _, e := range entries {
		for _, a := range e.Attributes {
			key := strings.ToLower(a.Name)
			if technicalAttrs[key] || !ValidAttrName(a.Name) || len(a.Values) == 0 {
				continue
			}
			v := strings.TrimSpace(a.Values[0])
			if v == "" || len(v) > 80 || !utf8.ValidString(v) || strings.ContainsAny(v, "\x00\x01\x02\x03") {
				continue
			}
			names[key] = a.Name
			counts[key]++
			if values[key] == nil {
				values[key] = map[string]int{}
			}
			values[key][v]++
		}
	}
	out := []AttrStat{}
	for key, n := range counts {
		st := AttrStat{Name: names[key], Count: n}
		vs := make([]string, 0, len(values[key]))
		for v := range values[key] {
			vs = append(vs, v)
		}
		sort.Slice(vs, func(i, j int) bool {
			if values[key][vs[i]] != values[key][vs[j]] {
				return values[key][vs[i]] > values[key][vs[j]]
			}
			return vs[i] < vs[j]
		})
		if len(vs) > 3 {
			vs = vs[:3]
		}
		st.Examples = vs
		out = append(out, st)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// Etichette proposte per gli attributi più comuni (modificabili nel form).
var defaultLabels = map[string]string{
	"physicaldeliveryofficename": "Ufficio",
	"department":                 "Reparto",
	"company":                    "Ente",
	"title":                      "Qualifica",
	"description":                "Descrizione",
	"l":                          "Città",
	"division":                   "Divisione",
	"employeetype":               "Tipo di dipendente",
}

// DefaultAttrLabel: etichetta suggerita per un attributo (il nome se non nota).
func DefaultAttrLabel(name string) string {
	if l, ok := defaultLabels[strings.ToLower(name)]; ok {
		return l
	}
	return name
}
