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

	const isCritical = (data.critical === true) || (data.critical === 'true');

	// vibration pattern for critical notifications (very strong)
	const heavyVibrate = [400, 120, 400, 120, 800];

	const options = {
		body: body,
		data: { url: url },
		// include a simple icon (provided by server)
		icon: data.icon || '/static/notification-icon.png',
		tag: isCritical ? 'hyperhive-critical' : Date.now().toString(),
		requireInteraction: !!isCritical,
		renotify: !!isCritical,
		vibrate: isCritical ? heavyVibrate : undefined,
	};

	// Debug log so you can inspect payload/options in ServiceWorker console
	console.log('[sw] showNotification options=', options, 'data=', data, 'isCritical=', isCritical);

	event.waitUntil(self.registration.showNotification(title, options));
});

self.addEventListener('notificationclick', function (event) {
	event.notification.close();
	const urlPath = (event.notification.data && event.notification.data.url) || '/';
	const fullUrl = self.location.origin + urlPath;

	event.waitUntil(clients.openWindow(fullUrl));
});
