// errori.js: manda al Cruscotto (POST /errori) gli errori JavaScript non
// gestiti; il server li ripulisce e li inoltra a GlitchTip. Incluso per primo,
// solo quando GlitchTip è configurato. Non deve mai disturbare la pagina.
(function () {
	"use strict";
	var MAX_INVII = 10;
	var inviati = 0;
	var visti = new Set();
	var ESTENSIONI = ["chrome-extension://", "moz-extension://", "safari-extension://", "safari-web-extension://", "edge-extension://"];

	function daEstensione(s) {
		if (!s) return false;
		for (var i = 0; i < ESTENSIONI.length; i++) {
			if (String(s).indexOf(ESTENSIONI[i]) >= 0) return true;
		}
		return false;
	}

	function delSito(file) {
		if (!file) return true;
		try {
			return new URL(file, location.origin).origin === location.origin;
		} catch (e) {
			return false;
		}
	}

	function invia(messaggio, file, riga, colonna, stack) {
		try {
			messaggio = String(messaggio || "").slice(0, 500);
			if (!messaggio || inviati >= MAX_INVII) return;
			if (messaggio === "Script error." && !file) return; // script di altri domini
			if (daEstensione(file) || daEstensione(stack) || !delSito(file)) return;
			var firma = messaggio + "|" + (file || "") + "|" + (riga || 0);
			if (visti.has(firma)) return;
			visti.add(firma);
			inviati++;
			var corpo = JSON.stringify({
				message: messaggio,
				stack: String(stack || "").slice(0, 8192),
				file: file || "",
				line: riga || 0,
				col: colonna || 0,
				page: location.pathname,
				ua: String(navigator.userAgent || "").slice(0, 300)
			});
			// text/plain: tipo ammesso da sendBeacon senza CORS (il server legge il JSON comunque).
			if (navigator.sendBeacon && navigator.sendBeacon("/errori", new Blob([corpo], { type: "text/plain" }))) return;
			fetch("/errori", {
				method: "POST",
				body: corpo,
				headers: { "Content-Type": "application/json" },
				keepalive: true,
				credentials: "same-origin"
			}).catch(function () {});
		} catch (e) {
			// mai un errore da qui
		}
	}

	window.addEventListener("error", function (e) {
		try {
			if (!e || e.target !== window && e.target && e.target.tagName) return; // risorse (img, script) non caricate
			invia(e.message, e.filename, e.lineno, e.colno, e.error && e.error.stack);
		} catch (x) {}
	});

	window.addEventListener("unhandledrejection", function (e) {
		try {
			var r = e && e.reason;
			var msg = r && r.message ? r.message : String(r);
			invia("Promise rifiutata: " + msg, "", 0, 0, r && r.stack);
		} catch (x) {}
	});
})();
