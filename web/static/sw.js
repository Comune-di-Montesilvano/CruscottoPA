// Service worker di CruscottoPA: notifiche push e installazione come app.
// Nessuna cache offline: la plancia ha senso solo online.
self.addEventListener("install", () => self.skipWaiting());
self.addEventListener("activate", (e) => e.waitUntil(self.clients.claim()));

self.addEventListener("push", (e) => {
	let d = {};
	try { d = e.data ? e.data.json() : {}; } catch (_) { d = { title: e.data && e.data.text() }; }
	e.waitUntil(self.registration.showNotification(d.title || "CruscottoPA", {
		body: d.body || "",
		icon: "/static/img/icon-192.png",
		badge: "/static/img/icon-192.png",
		tag: d.tag || "cruscottopa",
		renotify: true, // stesso tag di una notifica presente: torna a farsi notare
		data: { url: d.url || "/" },
	}));
});

self.addEventListener("notificationclick", (e) => {
	e.notification.close();
	const url = (e.notification.data && e.notification.data.url) || "/";
	e.waitUntil(self.clients.matchAll({ type: "window", includeUncontrolled: true }).then((list) => {
		for (const c of list) {
			const u = new URL(c.url);
			// Solo schede della plancia: una scheda "/admin" può avere lavoro non salvato.
			if (u.origin !== self.location.origin || u.pathname.startsWith("/admin")) continue;
			// navigate() fallisce sulle schede che questo service worker non
			// controlla (es. ricaricate forzando): allora si apre una finestra.
			return (url === "/" ? Promise.resolve(c) : c.navigate(url))
				.then((w) => (w || c).focus())
				.catch(() => self.clients.openWindow(url));
		}
		return self.clients.openWindow(url);
	}));
});
