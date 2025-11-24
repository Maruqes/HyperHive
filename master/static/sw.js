// sw.js

self.addEventListener('push', function (event) {
	let data = {};
	if (event.data) {
		try {
			data = event.data.json();
		} catch (e) {
			data = { title: 'Notification', body: event.data.text() };
		}
	}

	const title = data.title || 'Notification';
	const body  = data.body  || 'New notification';
	const url   = data.url   || '/';

	const options = {
		body: body,
		data: { url: url },
		// nada de ícones, vibrar, etc. – mesmo minimal
		tag: Date.now().toString(),
		requireInteraction: false,
	};

	event.waitUntil(self.registration.showNotification(title, options));
});

self.addEventListener('notificationclick', function (event) {
	event.notification.close();
	const urlPath = (event.notification.data && event.notification.data.url) || '/';
	const fullUrl = self.location.origin + urlPath;

	event.waitUntil(clients.openWindow(fullUrl));
});
