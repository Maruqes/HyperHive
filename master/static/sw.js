self.addEventListener('push', function (event) {
	// Simple push handler: parse payload and show a minimal notification
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
		// minimal: no icon, no badge, no vibrate, no image, no renotify
		tag: Date.now().toString(),
		requireInteraction: false,
	};

	event.waitUntil(self.registration.showNotification(title, options));
});

self.addEventListener('notificationclick', function (event) {
	event.notification.close();
	const urlPath = (event.notification.data && event.notification.data.url) || '/';
	// Build full URL dynamically so we don't hardcode domains
	const fullUrl = self.location.origin + urlPath;

	event.waitUntil(clients.openWindow(fullUrl));
});
