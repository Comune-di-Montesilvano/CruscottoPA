// Pannello admin: picker icone e anteprima colore. Nessun handler inline (CSP).
(() => {
	"use strict";

	document.addEventListener("click", (e) => {
		const btn = e.target.closest("[data-icon]");
		if (!btn) return;
		const form = btn.closest("form");
		form.querySelector("[name=icon_value_pack]").value = btn.dataset.icon;
		const icon = form.querySelector("[data-preview-icon]");
		if (icon) icon.textContent = btn.dataset.icon;
		form.querySelectorAll("[data-icon][aria-pressed]").forEach((b) => b.removeAttribute("aria-pressed"));
		btn.setAttribute("aria-pressed", "true");
	});

	document.addEventListener("input", (e) => {
		if (e.target.name !== "icon_color") return;
		const preview = e.target.form?.querySelector("[data-preview]");
		if (preview) preview.style.background = e.target.value;
	});

	// ── Ripristino da file: upload a pezzi (sotto il limite del reverse proxy) ──
	async function uploadRestore(form) {
		const file = form.querySelector("[name=archivio]").files[0];
		const conferma = form.querySelector("[name=conferma]").value;
		const bar = form.querySelector("progress");
		const msg = form.querySelector("[data-upload-msg]");
		const button = form.querySelector("button[type=submit]");
		msg.textContent = "";
		if (!file) { msg.textContent = "Scegli un file .tar.gz."; return; }
		if (conferma !== "RIPRISTINA") { msg.textContent = "Per confermare digita RIPRISTINA."; return; }
		button.disabled = true;
		try {
			const start = await fetch("/admin/backup/upload", { method: "POST" });
			if (!start.ok) throw new Error(start.status === 429 ? (await start.text()).trim() : "Impossibile avviare il caricamento.");
			const { id, chunk } = await start.json();
			const total = Math.max(1, Math.ceil(file.size / chunk));
			for (let n = 0; n < total; n++) {
				const res = await fetch(`/admin/backup/upload/${id}/chunk?n=${n}`, {
					method: "POST",
					body: file.slice(n * chunk, (n + 1) * chunk),
				});
				if (!res.ok) throw new Error(`Caricamento interrotto (pezzo ${n + 1} di ${total}): ${(await res.text()).trim()}`);
				bar.value = (n + 1) / total;
			}
			const fin = await fetch(`/admin/backup/upload/${id}/fine`, {
				method: "POST",
				body: new URLSearchParams({ conferma }),
			});
			document.getElementById("section").outerHTML = await fin.text();
			watchRestart();
		} catch (err) {
			msg.textContent = err.message;
			button.disabled = false;
		}
	}

	document.addEventListener("submit", (e) => {
		if (!e.target.matches("[data-restore-upload]")) return;
		e.preventDefault();
		uploadRestore(e.target);
	});

	// ── Dopo un ripristino: attende che il servizio riparta e ricarica ──
	function watchRestart() {
		if (!document.querySelector("#section[data-restarting]")) return;
		const started = Date.now();
		const poll = async () => {
			try {
				const res = await fetch("/health", { cache: "no-store" });
				if (res.ok && Date.now() - started > 3000) {
					location.href = "/admin/backup";
					return;
				}
			} catch (_) {
				// servizio in riavvio
			}
			setTimeout(poll, 2000);
		};
		setTimeout(poll, 2000);
	}
	document.addEventListener("htmx:afterSwap", watchRestart);

	// Suggerimenti AD: un click riempie il valore (e l'etichetta del gruppo AD).
	document.addEventListener("click", (e) => {
		const b = e.target.closest(".suggestion");
		if (!b) return;
		const form = b.closest("form");
		if (!form) return;
		const field = form.querySelector('[name="' + (b.dataset.field || "value") + '"]');
		if (field) field.value = b.dataset.fill || "";
		const label = form.querySelector('[name="label"]');
		if (label) label.value = b.dataset.label || "";
		b.parentElement.innerHTML = "";
	});
	// Il campo "Attributo" serve solo con il tipo "Attributo".
	document.addEventListener("change", (e) => {
		const sel = e.target.closest("[data-rule-kind]");
		if (!sel) return;
		sel.form.querySelectorAll("[data-for-kind]").forEach((el) => { el.hidden = el.dataset.forKind !== sel.value; });
	});

	// Visibilità dei contenuti: con "Pubblico" le caselle dei gruppi non servono.
	function syncVisibility(root) {
		root.querySelectorAll("fieldset.visibility").forEach((fs) => {
			const checked = fs.querySelector('[name="visibilita"]:checked');
			const isPublic = !checked || checked.value === "";
			fs.querySelectorAll('[name="gruppi"]').forEach((cb) => { cb.disabled = isPublic; });
		});
	}
	document.addEventListener("change", (e) => {
		if (e.target.matches('[name="visibilita"]')) syncVisibility(e.target.closest("form") || document);
	});
	document.addEventListener("htmx:afterSwap", (e) => syncVisibility(e.target));
	syncVisibility(document);

	// Errori delle azioni HTMX: messaggio generico a schermo. Non si usa il corpo
	// della risposta: in produzione il reverse proxy lo sostituisce con una
	// pagina di cortesia.
	function toast(text) {
		let box = document.getElementById("toast");
		if (!box) {
			box = document.createElement("div");
			box.id = "toast";
			box.className = "toast";
			box.setAttribute("role", "alert");
			document.body.appendChild(box);
		}
		box.textContent = text;
		box.hidden = false;
		clearTimeout(box.timer);
		box.timer = setTimeout(() => { box.hidden = true; }, 8000);
	}
	// "Invia notifica": proposta per gli urgenti, finché l'admin non la cambia.
	document.addEventListener("change", (e) => {
		const box = e.target.closest("[data-notify]");
		if (box) { box.dataset.touched = "1"; return; }
		if (e.target.name !== "level") return;
		const cb = e.target.form && e.target.form.querySelector("[data-notify]");
		if (cb && !cb.dataset.touched) cb.checked = e.target.value === "urgent";
	});

	document.addEventListener("htmx:responseError", (e) => {
		const status = e.detail.xhr ? e.detail.xhr.status : 0;
		const what = status === 404 ? "elemento non trovato (forse già eliminato)"
			: status === 403 ? "richiesta rifiutata"
			: "errore " + status;
		toast("Operazione non riuscita: " + what + ". Ricarica la pagina e riprova.");
	});
	document.addEventListener("htmx:sendError", () => {
		toast("Server non raggiungibile. Controlla la connessione e riprova.");
	});
})();
