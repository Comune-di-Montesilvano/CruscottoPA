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
	if (offBtn) {
		offBtn.addEventListener("click", async () => {
			if (Notification.permission === "denied") {
				if (blocked && !blocked.open) blocked.showModal();
				return;
			}
			const p = await Notification.requestPermission();
			if (p === "granted") await subscribe().catch(() => {});
			refreshOff();
		});
	}
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
	if ("EventSource" in window) {
		const bc = "BroadcastChannel" in window ? new BroadcastChannel("cruscotto-eventi") : null;
		const stream = () => new Promise(() => { // mai risolta: il lock resta finché la scheda è aperta
			const es = new EventSource("/eventi");
			es.addEventListener("avviso", (e) => {
				let a = {};
				try { a = JSON.parse(e.data); } catch (_) { return; }
				onAvviso(a, true);
				if (bc) bc.postMessage(a);
			});
		});
		if (bc && navigator.locks) {
			bc.onmessage = (e) => onAvviso(e.data, false);
			navigator.locks.request("cruscotto-eventi", stream);
		} else {
			stream();
		}
	}

	if (supported) {
		if (active()) subscribe().catch(() => {}); // anche se concesso da policy di dominio
		setTimeout(maybeAsk, 1500);
	}
})();
