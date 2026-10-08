"""Illustrazioni stilizzate dei browser per le guide della plancia."""
import os

OUT = os.path.join(os.path.dirname(os.path.abspath(__file__)), '..', 'web', 'static', 'img', 'guida')
os.makedirs(OUT, exist_ok=True)

W = 520
FONT = "font-family=\"Segoe UI, system-ui, sans-serif\""
RED = "#d92d20"
URL = "plancia.ente.it"

BROWSERS = {
    "chrome": {"bar": "#dfe3e8", "addr": "#f1f3f4", "radius": 17, "tab": "#ffffff", "accent": "#1a73e8"},
    "edge": {"bar": "#e9ecef", "addr": "#ffffff", "radius": 8, "tab": "#f7f7f7", "accent": "#0f6cbd"},
    "firefox": {"bar": "#e7e7ec", "addr": "#ffffff", "radius": 6, "tab": "#f9f9fb", "accent": "#0061e0"},
}


def frame(b, h, url=URL, icon=""):
    c = BROWSERS[b]
    return f'''<rect x="1" y="1" width="{W-2}" height="{h-2}" rx="12" fill="#ffffff" stroke="#c9ced6"/>
<path d="M1 13 a12 12 0 0 1 12 -12 h{W-26} a12 12 0 0 1 12 12 v27 h-{W-2} z" fill="{c['bar']}"/>
<circle cx="20" cy="20" r="5" fill="#ff5f57"/><circle cx="36" cy="20" r="5" fill="#febc2e"/><circle cx="52" cy="20" r="5" fill="#28c840"/>
<rect x="72" y="8" width="170" height="32" rx="8" fill="{c['tab']}"/>
<text x="86" y="29" font-size="12" fill="#3c4043" {FONT}>CruscottoPA</text>
<rect x="1" y="40" width="{W-2}" height="44" fill="{c['tab']}"/>
<text x="16" y="68" font-size="18" fill="#6b7280" {FONT}>&#8592;</text>
<text x="40" y="68" font-size="18" fill="#6b7280" {FONT}>&#8635;</text>
<rect x="70" y="48" width="{W-90}" height="28" rx="{c['radius']}" fill="{c['addr']}" stroke="#d0d5dd"/>
{icon}
<text x="112" y="67" font-size="13" fill="#202124" {FONT}>{url}</text>'''


def ring(cx, cy, r=17, n="1", nx=None, ny=None):
    nx = cx + r + 6 if nx is None else nx
    ny = cy - r - 2 if ny is None else ny
    return f'''<circle cx="{cx}" cy="{cy}" r="{r}" fill="none" stroke="{RED}" stroke-width="3"/>
<circle cx="{nx}" cy="{ny}" r="10" fill="{RED}"/><text x="{nx}" y="{ny+4}" font-size="12" font-weight="700" fill="#fff" text-anchor="middle" {FONT}>{n}</text>'''


def box(x, y, w, h, n, nx=None, ny=None):
    nx = x + w + 4 if nx is None else nx
    ny = y - 4 if ny is None else ny
    return f'''<rect x="{x}" y="{y}" width="{w}" height="{h}" rx="9" fill="none" stroke="{RED}" stroke-width="3"/>
<circle cx="{nx}" cy="{ny}" r="10" fill="{RED}"/><text x="{nx}" y="{ny+4}" font-size="12" font-weight="700" fill="#fff" text-anchor="middle" {FONT}>{n}</text>'''


def cursor(x, y):
    return f'<path d="M{x} {y} l0 18 l5 -5 l4 9 l3 -1.5 l-4 -8.5 l7 0 z" fill="#111827" stroke="#fff" stroke-width="1.2"/>'


def svg(name, h, body, title):
    with open(os.path.join(OUT, name), 'w', encoding='utf8', newline='\n') as f:
        f.write(f'''<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 {W} {h}" width="{W}" height="{h}" role="img" aria-label="{title}">
<title>{title}</title>
{body}
</svg>
''')


# Icone a sinistra dell'indirizzo.
ICON = {
    # Chrome: «Visualizza informazioni sul sito» (cursori).
    "chrome": '''<g stroke="#5f6368" stroke-width="2" stroke-linecap="round"><line x1="84" y1="57" x2="100" y2="57"/><line x1="84" y1="67" x2="100" y2="67"/></g>
<circle cx="89" cy="57" r="3" fill="#fff" stroke="#5f6368" stroke-width="2"/><circle cx="95" cy="67" r="3" fill="#fff" stroke="#5f6368" stroke-width="2"/>''',
    # Edge: lucchetto.
    "edge": '''<rect x="85" y="60" width="14" height="10" rx="2" fill="#5f6368"/><path d="M88 60 v-3 a4 4 0 0 1 8 0 v3" fill="none" stroke="#5f6368" stroke-width="2"/>''',
    # Firefox: campanella barrata (permessi bloccati).
    "firefox": '''<path d="M86 66 h14 l-2 -3 v-4 a5 5 0 0 0 -10 0 v4 z" fill="#5f6368"/><circle cx="93" cy="68" r="2" fill="#5f6368"/><line x1="84" y1="70" x2="102" y2="54" stroke="#d92d20" stroke-width="2"/>''',
}
STEP1 = {
    "chrome": "Chrome: clic sull'icona a sinistra dell'indirizzo",
    "edge": "Edge: clic sul lucchetto a sinistra dell'indirizzo",
    "firefox": "Firefox: clic sulla campanella barrata a sinistra dell'indirizzo",
}
for b in BROWSERS:
    h = 120
    body = frame(b, h, icon=ICON[b]) + ring(92, 62) + cursor(96, 66)
    svg(f"notifiche-{b}-1.svg", h, body, STEP1[b])


def panel(b, rows, h):
    c = BROWSERS[b]
    out = [frame(b, h, icon=ICON[b])]
    out.append(f'<rect x="70" y="82" width="300" height="{h-96}" rx="10" fill="#ffffff" stroke="#c9ced6"/>')
    out.append(f'<rect x="70" y="82" width="300" height="{h-96}" rx="10" fill="none" stroke="#000" stroke-opacity=".04" stroke-width="6"/>')
    return out


# Chrome: pannello informazioni sul sito, interruttore «Notifiche».
h = 230
b = "chrome"
p = panel(b, None, h)
p.append(f'<text x="88" y="110" font-size="13" fill="#202124" {FONT}>&#128274;  La connessione è sicura</text>')
p.append('<line x1="80" y1="124" x2="360" y2="124" stroke="#e5e7eb"/>')
p.append(f'<text x="88" y="152" font-size="14" fill="#202124" {FONT}>&#128276;  Notifiche</text>')
p.append(f'<rect x="300" y="138" width="40" height="22" rx="11" fill="{BROWSERS[b]["accent"]}"/><circle cx="329" cy="149" r="8" fill="#fff"/>')
p.append('<line x1="80" y1="172" x2="360" y2="172" stroke="#e5e7eb"/>')
p.append(f'<text x="88" y="198" font-size="13" fill="#5f6368" {FONT}>&#9881;  Impostazioni sito</text>')
p.append(ring(320, 149, 22, "2", 352, 124))
p.append(cursor(326, 152))
svg("notifiche-chrome-2.svg", h, "\n".join(p), "Chrome: accendi l'interruttore Notifiche, poi ricarica la pagina")

# Edge: autorizzazioni per il sito, «Notifiche: Consenti».
h = 230
b = "edge"
p = panel(b, None, h)
p.append(f'<text x="88" y="110" font-size="13" font-weight="600" fill="#202124" {FONT}>Autorizzazioni per questo sito</text>')
p.append(f'<text x="88" y="152" font-size="14" fill="#202124" {FONT}>&#128276;  Notifiche</text>')
p.append(f'<rect x="246" y="136" width="108" height="26" rx="6" fill="#fff" stroke="{BROWSERS[b]["accent"]}" stroke-width="1.5"/>')
p.append(f'<text x="258" y="154" font-size="13" fill="#202124" {FONT}>Consenti</text><path d="M336 146 l5 5 l5 -5" fill="none" stroke="#5f6368" stroke-width="1.6"/>')
p.append(f'<text x="88" y="198" font-size="13" fill="#5f6368" {FONT}>&#128247;  Fotocamera</text>')
p.append(f'<text x="258" y="198" font-size="13" fill="#5f6368" {FONT}>Chiedi</text>')
p.append(box(240, 130, 120, 38, "2"))
p.append(cursor(318, 154))
svg("notifiche-edge-2.svg", h, "\n".join(p), "Edge: alla voce Notifiche scegli Consenti, poi ricarica la pagina")

# Firefox: pannello autorizzazioni, togli il blocco con la X.
h = 210
b = "firefox"
p = panel(b, None, h)
p.append(f'<text x="88" y="110" font-size="13" font-weight="600" fill="#202124" {FONT}>Autorizzazioni</text>')
p.append(f'<text x="88" y="148" font-size="14" fill="#202124" {FONT}>&#128277;  Invio notifiche</text>')
p.append(f'<text x="236" y="148" font-size="12" fill="#b42318" {FONT}>Bloccato</text>')
p.append('<g stroke="#5f6368" stroke-width="2" stroke-linecap="round"><line x1="336" y1="138" x2="346" y2="148"/><line x1="346" y1="138" x2="336" y2="148"/></g>')
p.append(ring(341, 143, 16, "2", 362, 118))
p.append(cursor(343, 147))
svg("notifiche-firefox-2.svg", h, "\n".join(p), "Firefox: togli il blocco con la X, ricarica e premi Attiva")


# Firefox NTLM (riconoscimento).
def ff_page(h, url):
    return frame("firefox", h, url=url, icon='<circle cx="92" cy="62" r="6" fill="none" stroke="#5f6368" stroke-width="2"/>')


h = 230
body = [ff_page(h, "about:config")]
body.append(f'<text x="40" y="124" font-size="18" font-weight="700" fill="#202124" {FONT}>Procedere con cautela</text>')
body.append(f'<text x="40" y="150" font-size="13" fill="#4b5563" {FONT}>Modificare le impostazioni avanzate può avere effetti sul funzionamento.</text>')
body.append(f'<rect x="290" y="172" width="200" height="34" rx="6" fill="{BROWSERS["firefox"]["accent"]}"/>')
body.append(f'<text x="390" y="194" font-size="13" font-weight="600" fill="#fff" text-anchor="middle" {FONT}>Accetta il rischio e continua</text>')
body.append(box(282, 164, 216, 50, "1", 500, 160))
body.append(cursor(400, 194))
svg("firefox-ntlm-1.svg", h, "\n".join(body), "Firefox: scrivi about:config nella barra e accetta l'avviso")

h = 210
body = [ff_page(h, "about:config")]
body.append(f'<rect x="30" y="100" width="{W-60}" height="32" rx="6" fill="#fff" stroke="{BROWSERS["firefox"]["accent"]}" stroke-width="1.5"/>')
body.append(f'<text x="44" y="121" font-size="13" fill="#202124" {FONT}>network.automatic-ntlm-auth.trusted-uris</text>')
body.append(f'<rect x="30" y="148" width="{W-60}" height="36" fill="#f0f0f4"/>')
body.append(f'<text x="44" y="171" font-size="12" font-weight="600" fill="#202124" {FONT}>network.automatic-ntlm-auth.trusted-uris</text>')
body.append('<path d="M452 174 l10 -10 l4 4 l-10 10 h-4 z" fill="#5f6368"/>')
body.append(ring(458, 168, 16, "2", 480, 142))
body.append(cursor(460, 172))
svg("firefox-ntlm-2.svg", h, "\n".join(body), "Firefox: cerca network.automatic-ntlm-auth.trusted-uris e premi la matita")

h = 190
body = [ff_page(h, "about:config")]
body.append(f'<rect x="30" y="104" width="{W-60}" height="40" fill="#f0f0f4"/>')
body.append(f'<text x="44" y="129" font-size="12" font-weight="600" fill="#202124" {FONT}>network.automatic-ntlm-auth.trusted-uris</text>')
body.append(f'<rect x="300" y="112" width="140" height="24" rx="4" fill="#fff" stroke="{BROWSERS["firefox"]["accent"]}" stroke-width="1.5"/>')
body.append(f'<text x="308" y="129" font-size="12" fill="#202124" {FONT}>{URL}</text>')
body.append('<path d="M452 124 l5 5 l10 -11" fill="none" stroke="#1a7f37" stroke-width="3" stroke-linecap="round"/>')
body.append(box(292, 104, 156, 40, "3", 300, 100))
body.append(ring(459, 124, 14, "3", 482, 100))
svg("firefox-ntlm-3.svg", h, "\n".join(body), "Firefox: scrivi l'indirizzo del sito e conferma con la spunta")
print('ok', sorted(os.listdir(OUT)))
