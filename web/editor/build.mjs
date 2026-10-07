// Impacchetta l'editor in un solo file per web/static/vendor (OUT_DIR).
import { build } from "esbuild";

const out = process.env.OUT_DIR || "dist";
await build({
	entryPoints: ["src/editor.js"],
	bundle: true,
	minify: true,
	format: "iife",
	target: "es2022",
	outfile: `${out}/editor.js`,
	legalComments: "linked",
});
await build({
	entryPoints: ["src/editor.css"],
	bundle: true,
	minify: true,
	outfile: `${out}/editor.css`,
});
