// Ticket: dialog «Apri un ticket» in plancia e risposta nella pagina del
// ticket. Allegati a pezzi (il proxy taglia oltre 1 MB), invio come form;
// ogni risposta del server è 200 + JSON.
(function () {
	"use strict";
	const DRAFT = "cruscotto-ticket-bozza";

	const MSG = {
		limite: "Hai inviato molti messaggi nell'ultima ora: riprova più tardi",
		otrs: "Il sistema di assistenza non risponde: riprova",
		ad: "Non riesco a leggere i tuoi dati dalla rete del Comune: riprova tra poco",
		anonimo: "Il Cruscotto non ti riconosce più: ricarica la pagina",
		mail: "Nella rete del Comune manca la mail del tuo account",
		spento: "L'apertura dei ticket non è attiva",
		in_corso: "Stai già inviando un messaggio: attendi la risposta",
		non_tuo: "Questo ticket non è disponibile",
	};

	async function post(url, body, type) {
		const res = await fetch(url, { method: "POST", body: body, headers: type ? { "Content-Type": type } : {}, credentials: "same-origin" });
		return res.json();
	}

	// Errori dentro un contenitore (dialog o modulo di risposta).
	function showErr(root, campo, msg) {
		const el = root.querySelector('[data-campo="' + campo + '"]');
		if (!el) return;
		el.textContent = msg || "";
		el.hidden = !msg;
	}
	function clearErrors(root) {
		root.querySelectorAll("[data-campo]").forEach(function (el) { el.hidden = true; el.textContent = ""; });
		const g = root.querySelector("[data-ticket-error]");
		if (g) { g.hidden = true; g.textContent = ""; }
	}
	function general(root, msg) {
		const g = root.querySelector("[data-ticket-error]");
		g.textContent = msg;
		g.hidden = false;
	}
	function failure(root, r) {
		if (r.campi) {
			Object.keys(r.campi).forEach(function (k) { showErr(root, k, r.campi[k]); });
			return;
		}
		general(root, (MSG[r.errore] || "Invio non riuscito: riprova") + (r.casella ? " o scrivi a " + r.casella + "." : "."));
	}

	// Informazioni sul PC dal browser (il nome del PC lo mette il server).
	const pcInfo = { browser: "", sistema: "", schermo: screen.width + "×" + screen.height };
	const ua = navigator.userAgent;
	const m = ua.match(/Edg\/(\d+)/) || ua.match(/Firefox\/(\d+)/) || ua.match(/Chrome\/(\d+)/);
	if (m) pcInfo.browser = (m[0].startsWith("Edg") ? "Edge " : m[0].startsWith("Firefox") ? "Firefox " : "Chrome ") + m[1];
	pcInfo.sistema = /Windows NT 10/.test(ua) ? "Windows 10/11" : (navigator.platform || "");
	function showPC() {
		const el = document.querySelector("[data-pc-info]");
		if (el) el.textContent = [pcInfo.browser, pcInfo.sistema, "schermo " + pcInfo.schermo].filter(Boolean).join(" · ");
	}
	showPC();
	if (navigator.userAgentData && navigator.userAgentData.getHighEntropyValues) {
		navigator.userAgentData.getHighEntropyValues(["platformVersion"]).then(function (v) {
			if (navigator.userAgentData.platform === "Windows" && v.platformVersion) {
				pcInfo.sistema = parseInt(v.platformVersion, 10) >= 13 ? "Windows 11" : "Windows 10";
				showPC();
			}
		}).catch(function () {});
	}
	function pcParams(params) {
		params.set("browser", pcInfo.browser);
		params.set("sistema", pcInfo.sistema);
		params.set("schermo", pcInfo.schermo);
		return params;
	}

	// setupFiles: bottone «Allega», scelta dei file e incolla dentro root.
	// Restituisce l'elenco degli allegati caricati ({id, li}).
	function setupFiles(root, pasteTarget) {
		const files = [];
		const list = root.querySelector("[data-ticket-files]");
		const input = root.querySelector("[data-ticket-input]");
		async function upload(file) {
			if (files.length >= 3) { showErr(root, "allegati", "Massimo 3 allegati."); return; }
			const name = file.name || "screenshot.png";
			const li = document.createElement("li");
			li.textContent = name + " — 0%";
			list.append(li);
			try {
				const start = await post("/ticket/allegati", new URLSearchParams({ nome: name }));
				if (!start.ok) throw new Error(start.error);
				for (let n = 0, off = 0; off < file.size; n++, off += start.chunk) {
					const r = await post("/ticket/allegati/" + start.id + "/pezzo?n=" + n, file.slice(off, off + start.chunk), "application/octet-stream");
					if (!r.ok) throw new Error(r.error);
					li.textContent = name + " — " + Math.min(100, Math.round((off + start.chunk) * 100 / file.size)) + "%";
				}
				const fin = await post("/ticket/allegati/" + start.id + "/fine", new URLSearchParams());
				if (!fin.ok) throw new Error(fin.error);
				const entry = { id: fin.id, li: li };
				files.push(entry);
				li.textContent = fin.nome + " ";
				const x = document.createElement("button");
				x.type = "button";
				x.className = "ticket-remove";
				x.setAttribute("aria-label", "Togli " + fin.nome);
				x.textContent = "×";
				x.addEventListener("click", function () { files.splice(files.indexOf(entry), 1); li.remove(); });
				li.append(x);
			} catch (e) {
				li.remove();
				showErr(root, "allegati", (e && e.message) || "Caricamento non riuscito.");
			}
		}
		root.querySelector("[data-ticket-attach]").addEventListener("click", function () { input.click(); });
		input.addEventListener("change", function () {
			Array.from(input.files).forEach(upload);
			input.value = "";
		});
		(pasteTarget || root).addEventListener("paste", function (e) {
			const items = (e.clipboardData && e.clipboardData.files) || [];
			if (items.length === 0) return;
			e.preventDefault();
			Array.from(items).forEach(upload);
		});
		return files;
	}

	// --- Dialog «Apri un ticket» (plancia) ---
	const dlg = document.querySelector("dialog.ticket");
	if (dlg) {
		const form = dlg.querySelector("[data-ticket-form]");

		const readDraft = function () {
			try { return JSON.parse(sessionStorage.getItem(DRAFT) || "{}"); } catch (e) { return {}; }
		};
		const saveDraft = function () {
			const d = { oggetto: form.oggetto.value, descrizione: form.descrizione.value, telefono: form.telefono.value };
			try { sessionStorage.setItem(DRAFT, JSON.stringify(d)); } catch (e) { /* storage non disponibile */ }
		};
		const clearDraft = function () {
			try { sessionStorage.removeItem(DRAFT); } catch (e) { /* storage non disponibile */ }
		};

		// Delegato: anche i bottoni del widget «I miei ticket», caricato da HTMX.
		document.addEventListener("click", function (e) {
			if (!e.target.closest("[data-ticket-open]")) return;
			if (form) {
				const d = readDraft();
				if (d.oggetto) form.oggetto.value = d.oggetto;
				if (d.descrizione) form.descrizione.value = d.descrizione;
				if (d.telefono) form.telefono.value = d.telefono;
			}
			dlg.showModal();
		});
		dlg.addEventListener("click", function (e) {
			if (e.target.closest("[data-ticket-close]")) dlg.close();
		});

		if (form) {
			const files = setupFiles(dlg);
			form.addEventListener("input", function (e) {
				if (e.target.name) showErr(dlg, e.target.name, "");
				saveDraft();
			});
			form.addEventListener("submit", async function (e) {
				e.preventDefault();
				clearErrors(dlg);
				const btn = dlg.querySelector("[data-ticket-submit]");
				btn.disabled = true;
				const body = pcParams(new URLSearchParams({ oggetto: form.oggetto.value, descrizione: form.descrizione.value, telefono: form.telefono.value }));
				files.forEach(function (f) { body.append("allegato", f.id); });
				try {
					const r = await post("/ticket", body);
					if (r.ok) {
						clearDraft();
						dlg.querySelector("[data-ticket-number]").textContent = r.numero;
						files.splice(0).forEach(function (f) { f.li.remove(); });
						form.reset();
						form.hidden = true;
						dlg.querySelector("[data-ticket-done]").hidden = false;
						document.body.dispatchEvent(new Event("ticket")); // aggiorna il widget
						return;
					}
					failure(dlg, r);
				} catch (err) {
					general(dlg, "Invio non riuscito: controlla la connessione e riprova.");
				} finally {
					btn.disabled = false;
				}
			});
			dlg.addEventListener("close", function () {
				if (dlg.querySelector("[data-ticket-done]").hidden) return;
				files.splice(0).forEach(function (f) { f.li.remove(); });
				form.reset();
				clearErrors(dlg);
				form.hidden = false;
				dlg.querySelector("[data-ticket-done]").hidden = true;
			});
		}
	}

	// --- Risposta nella pagina del ticket ---
	const replyForm = document.querySelector("[data-ticket-reply]");
	if (replyForm) {
		const replyFiles = setupFiles(replyForm, document);
		replyForm.addEventListener("input", function (e) {
			if (e.target.name) showErr(replyForm, e.target.name, "");
		});
		replyForm.addEventListener("submit", async function (e) {
			e.preventDefault();
			clearErrors(replyForm);
			const btn = replyForm.querySelector("[data-ticket-submit]");
			btn.disabled = true;
			const body = pcParams(new URLSearchParams({ descrizione: replyForm.descrizione.value }));
			replyFiles.forEach(function (f) { body.append("allegato", f.id); });
			try {
				const r = await post("/ticket/" + replyForm.dataset.ticketReply + "/risposta", body);
				if (r.ok) { location.reload(); return; }
				failure(replyForm, r);
			} catch (err) {
				general(replyForm, "Invio non riuscito: controlla la connessione e riprova.");
			} finally {
				btn.disabled = false;
			}
		});
	}
})();
