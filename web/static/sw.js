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
		data: { url: d.url || "/" },
	}));
});

self.addEventListener("notificationclick", (e) => {
	e.notification.close();
	const url = (e.notification.data && e.notification.data.url) || "/";
	e.waitUntil(self.clients.matchAll({ type: "window", includeUncontrolled: true }).then((list) => {
		for (const c of list) {
			if (new URL(c.url).origin === self.location.origin) {
				// Una scheda della plancia è aperta: la si porta sull'avviso.
				return (url === "/" ? Promise.resolve(c) : c.navigate(url)).then((w) => (w || c).focus());
			}
		}
		return self.clients.openWindow(url);
	}));
});
