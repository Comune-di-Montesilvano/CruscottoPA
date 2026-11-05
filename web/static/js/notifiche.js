// Notifiche degli avvisi: flusso SSE a plancia aperta, iscrizione Web Push e
// popup che le richiede finché non sono attive. Nessun handler inline (CSP).
(() => {
	"use strict";
	const supported = "Notification" in window && "serviceWorker" in navigator;
	const ASK_KEY = "cruscotto-notifiche-non-ora"; // quando il popup è stato chiuso
	const ASK_DAYS = 7;

	function load(k) { try { return localStorage.getItem(k); } catch (_) { return null; } }
	function save(k, v) { try { localStorage.setItem(k, v); } catch (_) { /* si ripresenterà al prossimo accesso */ } }
	// Fino alla 0.6.0 il footer permetteva di disattivarle: quella scelta non vale più.
	try { localStorage.removeItem("cruscotto-notifiche-off"); } catch (_) { /* niente da togliere */ }

	function active() { return Notification.permission === "granted"; }

	function b64ToBytes(s) {
		const pad = "=".repeat((4 - (s.length % 4)) % 4);
		const raw = atob((s + pad).replace(/-/g, "+").replace(/_/g, "/"));
		return Uint8Array.from(raw, (c) => c.charCodeAt(0));
	}

	async function subscribe() {
		const reg = await navigator.serviceWorker.register("/sw.js");
		const res = await fetch("/push/chiave", { credentials: "same-origin" });
		if (!res.ok) return; // Web Push spento: restano le notifiche a plancia aperta
		const text = (await res.text()).trim();
		if (!/^[A-Za-z0-9_-]{40,}$/.test(text)) return; // pagina di cortesia del proxy, non una chiave
		const key = b64ToBytes(text);
		let sub = await reg.pushManager.getSubscription();
		if (sub && !sameKey(sub.options.applicationServerKey, key)) {
			// Chiavi del server cambiate (es. ripristino): la vecchia iscrizione
			// non riceverebbe più nulla, va rifatta.
			await forget(sub);
			sub = null;
		}
		if (!sub) {
			sub = await reg.pushManager.subscribe({ userVisibleOnly: true, applicationServerKey: key });
		}
		await fetch("/push/iscrizioni", {
			method: "POST", credentials: "same-origin",
			headers: { "Content-Type": "application/json" },
			body: JSON.stringify(sub.toJSON()),
		});
		const badge = document.querySelector('[data-badge="notifiche"]');
		if (badge) {
			badge.classList.add("earned");
			badge.dataset.hint = badge.title;
		}
	}

	function sameKey(buf, key) {
		if (!buf) return false;
		const a = new Uint8Array(buf);
		return a.length === key.length && a.every((b, i) => b === key[i]);
	}

	// forget: toglie l'iscrizione dal server e dal browser.
	async function forget(sub) {
		await fetch("/push/iscrizioni/rimuovi", {
			method: "POST", credentials: "same-origin",
			headers: { "Content-Type": "application/json" },
			body: JSON.stringify({ endpoint: sub.endpoint }),
		}).catch(() => {});
		await sub.unsubscribe();
	}

	function askedRecently() {
		return Date.now() - (Number(load(ASK_KEY)) || 0) < ASK_DAYS * 864e5;
	}

	// Popup ogni ASK_DAYS giorni finché le notifiche non sono attive: chiede il
	// permesso (la richiesta del browser parte dal clic) o, se il browser le ha
	// bloccate, spiega come sbloccarle.
	// Guide illustrate: solo i passi del browser in uso (Chrome se non riconosciuto).
	function browserName() {
		const ua = navigator.userAgent;
		if (/Edg\//.test(ua)) return "edge";
		if (/Firefox\//.test(ua)) return "firefox";
		return "chrome";
	}
	const browser = browserName();
	document.querySelectorAll("[data-browser]").forEach((el) => { el.hidden = el.dataset.browser !== browser; });

	const ask = document.querySelector("dialog.notify-ask:not(.notify-blocked)");
	const blocked = document.querySelector("dialog.notify-blocked");
	function notNow() { save(ASK_KEY, String(Date.now())); }
	function maybeAsk() {
		if (!supported || active() || askedRecently()) return;
		const d = Notification.permission === "denied" ? blocked : ask;
		if (!d) return;
		if (document.querySelector("dialog[open]")) { setTimeout(maybeAsk, 3000); return; } // prima l'avviso urgente
		d.showModal();
	}
	if (ask) {
		ask.querySelector("[data-notify-yes]").addEventListener("click", async () => {
			ask.close();
			const p = await Notification.requestPermission();
			if (p === "granted") await subscribe().catch(() => {});
			else notNow();
			refreshOff();
		});
	}
	// Avviso fisso nella testata finché le notifiche non sono attive: al clic
	// chiede il permesso o, se il browser le ha bloccate, spiega come sbloccarle.
	const offBtn = document.querySelector("[data-notify-off]");
	function refreshOff() {
		if (offBtn) offBtn.hidden = !supported || active();
	}
	refreshOff();
	// Anche la spilla «Notifiche attive» non ancora presa chiede il permesso.
	document.addEventListener("click", async (e) => {
		if (!supported || !e.target.closest?.('[data-notify-off], [data-badge="notifiche"]:not(.earned)')) return;
		if (Notification.permission === "denied") {
			if (blocked && !blocked.open) blocked.showModal();
			return;
		}
		const p = await Notification.requestPermission();
		if (p === "granted") await subscribe().catch(() => {});
		refreshOff();
	});
	// Permesso cambiato dalle impostazioni del browser (es. sbloccato dal lucchetto).
	if (supported && navigator.permissions) {
		navigator.permissions.query({ name: "notifications" }).then((st) => {
			st.onchange = () => {
				refreshOff();
				if (active()) subscribe().catch(() => {});
			};
		}).catch(() => { /* API non disponibile: si aggiorna al prossimo caricamento */ });
	}

	for (const d of [ask, blocked]) {
		if (!d) continue;
		d.querySelector("[data-notify-no]").addEventListener("click", () => { notNow(); d.close(); });
		d.addEventListener("cancel", notNow); // Esc = "Non ora"
	}

	// Plancia aperta: eventi in tempo reale. Una sola connessione per browser
	// (il proxy parla HTTP/1.1: 6 connessioni per sito al massimo): la scheda
	// che ottiene il lock tiene il flusso e inoltra gli eventi alle altre.
	function onAvviso(a, leader) {
		if (window.htmx && document.getElementById("alerts")) {
			htmx.ajax("GET", "/partials/alerts", { target: "#alerts", swap: "innerHTML" });
		}
		if (leader && supported && active() && document.visibilityState !== "visible") {
			const opts = { body: "", icon: "/static/img/icon-192.png", tag: "avviso-" + a.id, data: { url: "/avvisi/" + a.id } };
			navigator.serviceWorker.getRegistration().then((reg) => {
				if (reg) reg.showNotification(a.title, opts); else new Notification(a.title, opts);
			});
		}
	}
	function onTicket(t, leader) {
		document.body.dispatchEvent(new Event("ticket")); // aggiorna il widget (hx-trigger "ticket from:body")
		if (leader && supported && active() && document.visibilityState !== "visible") {
			const opts = { body: t.body || "", icon: "/static/img/icon-192.png", tag: "ticket-" + t.id, data: { url: t.url } };
			navigator.serviceWorker.getRegistration().then((reg) => {
				if (reg) reg.showNotification(t.title, opts); else new Notification(t.title, opts);
			});
		}
	}
	// Avvisi e ticket ricaricati dal server: dopo un buco nel flusso o una scheda
	// rimasta a lungo in secondo piano, gli eventi persi non tornano da soli.
	function refreshAll() {
		if (window.htmx && document.getElementById("alerts")) {
			htmx.ajax("GET", "/partials/alerts", { target: "#alerts", swap: "innerHTML" });
		}
		document.body.dispatchEvent(new Event("ticket"));
	}
	if ("EventSource" in window) {
		const bc = "BroadcastChannel" in window ? new BroadcastChannel("cruscotto-eventi") : null;
		// Il server manda un ping ogni 25 s: senza ping per STALE_MS il flusso è
		// morto anche se il browser non lo sa (PC sospeso, cambio di rete, proxy).
		const STALE_MS = 70000;
		const RETRY_MIN = 5000, RETRY_MAX = 60000;
		const stream = () => new Promise(() => { // mai risolta: il lock resta finché la scheda è aperta
			let es = null, last = Date.now(), delay = RETRY_MIN, lost = false, timer = null;
			const alive = () => {
				last = Date.now();
				delay = RETRY_MIN;
				if (lost) { // tornato dopo un buco: si recupera quello che è cambiato
					lost = false;
					refreshAll();
					if (bc) bc.postMessage({ kind: "refresh" });
				}
			};
			const retry = (wait) => {
				if (es) { es.close(); es = null; }
				lost = true;
				clearTimeout(timer);
				timer = setTimeout(connect, wait);
				delay = Math.min(delay * 2, RETRY_MAX);
			};
			function connect() {
				clearTimeout(timer);
				if (es) es.close();
				last = Date.now();
				es = new EventSource("/eventi");
				es.addEventListener("ping", alive);
				es.addEventListener("avviso", (e) => {
					alive();
					let a = {};
					try { a = JSON.parse(e.data); } catch (_) { return; }
					onAvviso(a, true);
					if (bc) bc.postMessage(a);
				});
				es.addEventListener("ticket", (e) => {
					alive();
					let t = {};
					try { t = JSON.parse(e.data); } catch (_) { return; }
					onTicket(t, true);
					if (bc) bc.postMessage(t);
				});
				es.addEventListener("error", () => {
					lost = true;
					// Risposta non SSE (es. la pagina di cortesia del proxy durante un
					// riavvio): il browser chiude per sempre, si riprova da qui.
					if (es && es.readyState === EventSource.CLOSED) retry(delay + Math.random() * delay / 2);
				});
			}
			setInterval(() => { if (es && Date.now() - last > STALE_MS) retry(Math.random() * RETRY_MIN); }, 15000); // es nullo = retry già in attesa
			// Rete tornata o scheda di nuovo in primo piano (anche dopo la sospensione
			// del PC, quando i timer erano fermi): si controlla subito.
			const check = () => { if (!es || lost || Date.now() - last > STALE_MS) retry(0); };
			window.addEventListener("online", check);
			document.addEventListener("visibilitychange", () => { if (document.visibilityState === "visible") check(); });
			connect();
		});
		if (bc && navigator.locks) {
			bc.onmessage = (e) => {
				const k = e.data && e.data.kind;
				if (k === "refresh") refreshAll();
				else if (k === "ticket") onTicket(e.data, false);
				else onAvviso(e.data, false);
			};
			navigator.locks.request("cruscotto-eventi", stream);
		} else {
			stream();
		}
	}
	// Scheda rimasta a lungo nascosta o congelata (schede in sospensione di Edge):
	// al ritorno si ricaricano avvisi e ticket senza aspettare il prossimo giro.
	const HIDDEN_REFRESH_MS = 5 * 60000;
	let hiddenAt = 0;
	document.addEventListener("visibilitychange", () => {
		if (document.visibilityState === "hidden") { hiddenAt = Date.now(); return; }
		if (hiddenAt && Date.now() - hiddenAt > HIDDEN_REFRESH_MS) refreshAll();
		hiddenAt = 0;
	});

	if (supported) {
		// Anche se concesso da policy di dominio. Una rete lenta all'apertura non
		// deve lasciare il browser senza iscrizione fino al prossimo caricamento.
		if (active()) subscribe().catch(() => setTimeout(() => subscribe().catch(() => {}), 60000));
		setTimeout(maybeAsk, 1500);
	}
})();
