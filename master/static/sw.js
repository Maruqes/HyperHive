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
	const body = data.body || 'New notification';
	const url = data.url || '/';

	const options = {
		body: body,
		data: { url: url },
		// include a simple icon (provided by server) — no vibration, no extras
		icon: data.icon || '/static/notification-icon.png',
		tag: Date.now().toString(),
		// request that the notification remains visible until user interacts;
		// this increases chances of a persistent / expanded heads-up on Android.
		requireInteraction: true,
		renotify: true,
	};

	// Debug log so you can inspect payload/options in ServiceWorker console
	console.log('[sw] showNotification options=', options, 'data=', data);

	event.waitUntil(self.registration.showNotification(title, options));
});

self.addEventListener('notificationclick', function (event) {
	event.notification.close();
	const urlPath = (event.notification.data && event.notification.data.url) || '/';
	const fullUrl = self.location.origin + urlPath;

	event.waitUntil(clients.openWindow(fullUrl));
});
