// Dialog «Apri un ticket»: allegati a pezzi (il proxy taglia oltre 1 MB),
// invio come form; ogni risposta del server è 200 + JSON.
(function () {
	"use strict";
	const dlg = document.querySelector("dialog.ticket");
	const opener = document.querySelector("[data-ticket-open]");
	if (!dlg || !opener) return;
	const form = dlg.querySelector("[data-ticket-form]");
	const DRAFT = "cruscotto-ticket-bozza";
	const files = []; // {id, nome, li}

	function readDraft() {
		try { return JSON.parse(sessionStorage.getItem(DRAFT) || "{}"); } catch (e) { return {}; }
	}
	function saveDraft() {
		if (!form) return;
		const d = { oggetto: form.oggetto.value, descrizione: form.descrizione.value, telefono: form.telefono.value };
		try { sessionStorage.setItem(DRAFT, JSON.stringify(d)); } catch (e) { /* storage non disponibile */ }
	}
	function clearDraft() {
		try { sessionStorage.removeItem(DRAFT); } catch (e) { /* storage non disponibile */ }
	}

	function showErr(campo, msg) {
		const el = dlg.querySelector('[data-campo="' + campo + '"]');
		if (!el) return;
		el.textContent = msg || "";
		el.hidden = !msg;
	}
	function clearErrors() {
		dlg.querySelectorAll("[data-campo]").forEach(function (el) { el.hidden = true; el.textContent = ""; });
		const g = dlg.querySelector("[data-ticket-error]");
		if (g) { g.hidden = true; g.textContent = ""; }
	}
	function general(msg) {
		const g = dlg.querySelector("[data-ticket-error]");
		g.textContent = msg;
		g.hidden = false;
	}

	async function post(url, body, type) {
		const res = await fetch(url, { method: "POST", body: body, headers: type ? { "Content-Type": type } : {}, credentials: "same-origin" });
		return res.json();
	}

	async function upload(file) {
		if (files.length >= 3) { showErr("allegati", "Massimo 3 allegati."); return; }
		const li = document.createElement("li");
		li.textContent = (file.name || "screenshot.png") + " — 0%";
		dlg.querySelector("[data-ticket-files]").append(li);
		try {
			const start = await post("/ticket/allegati", new URLSearchParams({ nome: file.name || "screenshot.png" }));
			if (!start.ok) throw new Error(start.error);
			for (let n = 0, off = 0; off < file.size; n++, off += start.chunk) {
				const r = await post("/ticket/allegati/" + start.id + "/pezzo?n=" + n, file.slice(off, off + start.chunk), "application/octet-stream");
				if (!r.ok) throw new Error(r.error);
				li.textContent = (file.name || "screenshot.png") + " — " + Math.min(100, Math.round((off + start.chunk) * 100 / file.size)) + "%";
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
			showErr("allegati", (e && e.message) || "Caricamento non riuscito.");
		}
	}

	function resetForm() {
		files.splice(0).forEach(function (f) { f.li.remove(); });
		form.reset();
		clearErrors();
		form.hidden = false;
		dlg.querySelector("[data-ticket-done]").hidden = true;
	}

	opener.addEventListener("click", function () {
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
	if (!form) return;

	form.addEventListener("input", saveDraft);
	const input = dlg.querySelector("[data-ticket-input]");
	dlg.querySelector("[data-ticket-attach]").addEventListener("click", function () { input.click(); });
	input.addEventListener("change", function () {
		Array.from(input.files).forEach(upload);
		input.value = "";
	});
	dlg.addEventListener("paste", function (e) {
		const items = (e.clipboardData && e.clipboardData.files) || [];
		if (items.length === 0) return;
		e.preventDefault();
		Array.from(items).forEach(upload);
	});

	const MSG = {
		limite: "Hai aperto molti ticket nell'ultima ora: riprova più tardi",
		otrs: "Il sistema di assistenza non risponde: riprova",
		ad: "Non riesco a leggere i tuoi dati dalla rete del Comune: riprova tra poco",
		anonimo: "Il Cruscotto non ti riconosce più: ricarica la pagina",
		mail: "Nella rete del Comune manca la mail del tuo account",
		spento: "L'apertura dei ticket non è attiva",
	};

	form.addEventListener("submit", async function (e) {
		e.preventDefault();
		clearErrors();
		const btn = dlg.querySelector("[data-ticket-submit]");
		btn.disabled = true;
		const body = new URLSearchParams({ oggetto: form.oggetto.value, descrizione: form.descrizione.value, telefono: form.telefono.value });
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
				return;
			}
			if (r.campi) {
				Object.keys(r.campi).forEach(function (k) { showErr(k, r.campi[k]); });
				return;
			}
			let msg = (MSG[r.errore] || "Invio non riuscito: riprova");
			msg += r.casella ? " o scrivi a " + r.casella + "." : ".";
			general(msg);
		} catch (err) {
			general("Invio non riuscito: controlla la connessione e riprova.");
		} finally {
			btn.disabled = false;
		}
	});
	dlg.addEventListener("close", function () {
		if (!dlg.querySelector("[data-ticket-done]").hidden) resetForm();
	});
})();
