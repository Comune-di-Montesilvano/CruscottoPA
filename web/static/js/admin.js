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
})();
