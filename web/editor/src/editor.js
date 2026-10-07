// Editor visuale di avvisi e guide: sostituisce le <textarea data-editor> e
// riscrive il Markdown nella textarea a ogni modifica, così i form HTMX
// restano identici. Senza questo file l'admin usa la textarea normale.
import { Editor } from "@tiptap/core";
import StarterKit from "@tiptap/starter-kit";
import Image from "@tiptap/extension-image";
import { TableKit } from "@tiptap/extension-table";
import { Placeholder } from "@tiptap/extensions";
import { Markdown } from "@tiptap/markdown";

// [etichetta, icona Material, azione, attivo?, visibile?]
const BUTTONS = [
	["Grassetto", "format_bold", (e) => e.chain().focus().toggleBold().run(), (e) => e.isActive("bold")],
	["Corsivo", "format_italic", (e) => e.chain().focus().toggleItalic().run(), (e) => e.isActive("italic")],
	["Titolo", "title", (e) => e.chain().focus().toggleHeading({ level: 2 }).run(), (e) => e.isActive("heading", { level: 2 })],
	["Sottotitolo", "text_fields", (e) => e.chain().focus().toggleHeading({ level: 3 }).run(), (e) => e.isActive("heading", { level: 3 })],
	["Elenco puntato", "format_list_bulleted", (e) => e.chain().focus().toggleBulletList().run(), (e) => e.isActive("bulletList")],
	["Elenco numerato", "format_list_numbered", (e) => e.chain().focus().toggleOrderedList().run(), (e) => e.isActive("orderedList")],
	["Link", "link", linkPrompt, (e) => e.isActive("link")],
	["Immagine", "image", pickImage],
	["Tabella", "table_chart", (e) => e.chain().focus().insertTable({ rows: 3, cols: 3, withHeaderRow: true }).run()],
	["Aggiungi riga", "table_rows", (e) => e.chain().focus().addRowAfter().run(), null, (e) => e.isActive("table")],
	["Aggiungi colonna", "view_column", (e) => e.chain().focus().addColumnAfter().run(), null, (e) => e.isActive("table")],
	["Elimina riga", "remove", (e) => e.chain().focus().deleteRow().run(), null, (e) => e.isActive("table")],
	["Elimina tabella", "grid_off", (e) => e.chain().focus().deleteTable().run(), null, (e) => e.isActive("table")],
	["Annulla", "undo", (e) => e.chain().focus().undo().run()],
	["Ripeti", "redo", (e) => e.chain().focus().redo().run()],
];

function linkPrompt(e) {
	const prev = e.getAttributes("link").href || "https://";
	const href = window.prompt("Indirizzo del link (vuoto per toglierlo):", prev);
	if (href === null) return;
	if (href.trim() === "") e.chain().focus().extendMarkRange("link").unsetLink().run();
	else e.chain().focus().extendMarkRange("link").setLink({ href: href.trim() }).run();
}

// insertImages carica le immagini a pezzi (admin.js) e le inserisce.
async function insertImages(e, files, status) {
	const up = window.cruscottoUpload;
	for (const f of files) {
		if (!f.type.startsWith("image/")) continue;
		if (!up) { status.textContent = "Caricamento non disponibile."; return; }
		status.textContent = "Caricamento immagine…";
		try {
			const r = await up.media(f, "immagine");
			e.chain().focus().setImage({ src: r.url, alt: f.name.replace(/\.[^.]+$/, "") }).run();
			status.textContent = "";
		} catch (err) {
			status.textContent = up.message(err);
		}
	}
}

function pickImage(e, status) {
	const input = document.createElement("input");
	input.type = "file";
	input.accept = "image/png,image/jpeg,image/webp";
	input.multiple = true;
	input.addEventListener("change", () => insertImages(e, [...input.files], status));
	input.click();
}

function mount(ta) {
	if (ta.dataset.editorReady) return;
	ta.dataset.editorReady = "1";
	const wrap = document.createElement("div");
	wrap.className = "md-editor";
	const bar = document.createElement("div");
	bar.className = "md-toolbar";
	bar.setAttribute("role", "toolbar");
	bar.setAttribute("aria-label", "Formattazione");
	const area = document.createElement("div");
	const status = document.createElement("p");
	status.className = "md-status hint";
	status.setAttribute("aria-live", "polite");
	wrap.append(bar, area, status);
	// Dentro la <label> l'editor riceverebbe i clic sull'etichetta: va dopo.
	(ta.closest("label") || ta).after(wrap);
	ta.hidden = true;
	ta.removeAttribute("required"); // il server valida comunque

	const label = (ta.closest("label")?.firstChild?.textContent || "Testo").trim();
	const editor = new Editor({
		element: area,
		injectCSS: false, // niente <style> inline (CSP): gli stili sono in editor.css
		extensions: [
			StarterKit.configure({ heading: { levels: [2, 3] }, link: { openOnClick: false, autolink: true } }),
			Image,
			TableKit.configure({ table: { resizable: false } }),
			Placeholder.configure({ placeholder: "Scrivi qui…" }),
			Markdown,
		],
		content: ta.value,
		contentType: "markdown",
		editorProps: {
			attributes: { class: "md md-editor-area", "aria-label": label, "aria-multiline": "true", role: "textbox" },
			handlePaste: (view, ev) => {
				const files = [...(ev.clipboardData?.files || [])].filter((f) => f.type.startsWith("image/"));
				if (!files.length) return false;
				insertImages(editor, files, status);
				return true;
			},
			handleDrop: (view, ev) => {
				const files = [...(ev.dataTransfer?.files || [])].filter((f) => f.type.startsWith("image/"));
				if (!files.length) return false;
				ev.preventDefault();
				insertImages(editor, files, status);
				return true;
			},
		},
		onUpdate: ({ editor: ed }) => { ta.value = ed.getMarkdown(); },
	});

	const buttons = BUTTONS.map(([text, icon, run, active, shown]) => {
		const b = document.createElement("button");
		b.type = "button";
		b.title = text;
		b.setAttribute("aria-label", text);
		const i = document.createElement("span");
		i.className = "material-icons";
		i.setAttribute("aria-hidden", "true");
		i.textContent = icon;
		b.append(i);
		b.addEventListener("mousedown", (ev) => ev.preventDefault()); // il focus resta nel testo
		b.addEventListener("click", () => run(editor, status));
		bar.append(b);
		return { b, active, shown };
	});
	const sync = () => {
		for (const { b, active, shown } of buttons) {
			if (active) b.setAttribute("aria-pressed", active(editor) ? "true" : "false");
			if (shown) b.hidden = !shown(editor);
		}
	};
	editor.on("transaction", sync);
	sync();
}

function mountAll(root) {
	root.querySelectorAll("textarea[data-editor]").forEach(mount);
}
document.addEventListener("htmx:afterSwap", (e) => mountAll(e.target));
mountAll(document);
