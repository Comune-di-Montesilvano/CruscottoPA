// Notifiche degli avvisi: flusso SSE a plancia aperta, iscrizione Web Push e
// popup di primo accesso. Nessun handler inline (CSP).
(() => {
	"use strict";
	const supported = "Notification" in window && "serviceWorker" in navigator;
	const ASK_KEY = "cruscotto-notifiche-non-ora"; // quando è stato scelto "Non ora"
	const OFF_KEY = "cruscotto-notifiche-off";     // disattivate dal link nel footer
	const ASK_DAYS = 30;

	function load(k) { try { return localStorage.getItem(k); } catch (_) { return null; } }
	function save(k, v) { try { if (v === null) localStorage.removeItem(k); else localStorage.setItem(k, v); } catch (_) { /* resta per questa pagina */ } }

	// Attive = permesso concesso e non disattivate dal footer.
	function active() { return Notification.permission === "granted" && !load(OFF_KEY); }

	function b64ToBytes(s) {
		const pad = "=".repeat((4 - (s.length % 4)) % 4);
		const raw = atob((s + pad).replace(/-/g, "+").replace(/_/g, "/"));
		return Uint8Array.from(raw, (c) => c.charCodeAt(0));
	}

	async function subscribe() {
		const reg = await navigator.serviceWorker.register("/sw.js");
		const key = await fetch("/push/chiave", { credentials: "same-origin" });
		if (!key.ok) return; // Web Push spento: restano le notifiche a plancia aperta
		const text = (await key.text()).trim();
		if (!/^[A-Za-z0-9_-]{40,}$/.test(text)) return; // pagina di cortesia del proxy, non una chiave
		let sub = await reg.pushManager.getSubscription();
		if (!sub) {
			sub = await reg.pushManager.subscribe({ userVisibleOnly: true, applicationServerKey: b64ToBytes(text) });
		}
		await fetch("/push/iscrizioni", {
			method: "POST", credentials: "same-origin",
			headers: { "Content-Type": "application/json" },
			body: JSON.stringify(sub.toJSON()),
		});
	}

	async function unsubscribe() {
		const reg = await navigator.serviceWorker.getRegistration();
		const sub = reg && await reg.pushManager.getSubscription();
		if (!sub) return;
		await fetch("/push/iscrizioni/rimuovi", {
			method: "POST", credentials: "same-origin",
			headers: { "Content-Type": "application/json" },
			body: JSON.stringify({ endpoint: sub.endpoint }),
		});
		await sub.unsubscribe();
	}

	function askedRecently() {
		return Date.now() - (Number(load(ASK_KEY)) || 0) < ASK_DAYS * 864e5;
	}

	async function enable() {
		save(OFF_KEY, null);
		const p = await Notification.requestPermission();
		if (p === "granted") await subscribe().catch(() => {});
		updateLink();
	}

	const link = document.querySelector("[data-notifiche]");
	function updateLink() {
		if (!link || !supported) return;
		link.parentElement.hidden = false;
		link.textContent = active() ? "Notifiche: disattiva" : "Notifiche: attiva";
		link.title = Notification.permission === "denied" ? "Bloccate dal browser: riattivale dalle impostazioni del sito (icona del lucchetto)" : "";
	}
	if (link) {
		link.addEventListener("click", async (e) => {
			e.preventDefault();
			if (active()) {
				save(OFF_KEY, "1");
				await unsubscribe().catch(() => {});
			} else if (Notification.permission === "granted") {
				save(OFF_KEY, null);
				await subscribe().catch(() => {});
			} else if (Notification.permission === "default") {
				await enable();
			}
			updateLink();
		});
	}

	// Popup di primo accesso: la richiesta del browser parte dal clic.
	const ask = document.querySelector("dialog.notify-ask");
	function notNow() { save(ASK_KEY, String(Date.now())); }
	function maybeAsk() {
		if (!supported || !ask || Notification.permission !== "default" || askedRecently()) return;
		if (document.querySelector("dialog[open]")) { setTimeout(maybeAsk, 3000); return; } // prima l'avviso urgente
		ask.showModal();
	}
	if (ask) {
		ask.querySelector("[data-notify-yes]").addEventListener("click", async () => { ask.close(); await enable(); });
		ask.querySelector("[data-notify-no]").addEventListener("click", () => { notNow(); ask.close(); });
		ask.addEventListener("cancel", notNow); // Esc = "Non ora"
	}

	// Plancia aperta: eventi in tempo reale.
	if ("EventSource" in window) {
		const es = new EventSource("/eventi");
		es.addEventListener("avviso", (e) => {
			let a = {};
			try { a = JSON.parse(e.data); } catch (_) { return; }
			if (window.htmx && document.getElementById("alerts")) {
				htmx.ajax("GET", "/partials/alerts", { target: "#alerts", swap: "innerHTML" });
			}
			if (supported && active() && document.visibilityState !== "visible") {
				const opts = { body: "", icon: "/static/img/icon-192.png", tag: "avviso-" + a.id, data: { url: "/" } };
				navigator.serviceWorker.getRegistration().then((reg) => {
					if (reg) reg.showNotification(a.title, opts); else new Notification(a.title, opts);
				});
			}
		});
	}

	if (supported) {
		updateLink();
		if (active()) subscribe().catch(() => {}); // anche se concesso da policy di dominio
		setTimeout(maybeAsk, 1500);
	}
})();
