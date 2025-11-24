self.addEventListener('push', function (event) {
	// Handle incoming push event and show a notification.
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
	const url = data.url || '/'; // relative path provided by backend

	// Normalize icon/badge to absolute URLs so fetch debugging works.
	const iconPath = data.icon || '/static/notification-icon.png';
	const badgePath = data.badge || '/static/notification-badge.png';
	const iconUrl = (iconPath && iconPath.startsWith('http')) ? iconPath : (self.location.origin + iconPath);
	const badgeUrl = (badgePath && badgePath.startsWith('http')) ? badgePath : (self.location.origin + badgePath);

	// Severidade (vem do backend: "critical", "warning", "info", etc.)
	const severity = data.severity || 'info';

	// Vibração: muito forte para critical, normal para o resto
	let vibratePattern = undefined;
	if (severity === 'critical') {
		// treme MUITO
		vibratePattern = [400, 120, 400, 120, 800];
	} else {
		// treme “normalzinho”
		vibratePattern = [200, 100, 200];
	}

	const options = {
		body,
		data: { url },
		icon: iconUrl,
		badge: badgeUrl,
		image: data.image || iconUrl,

		// popup desaparece após alguns segundos
		requireInteraction: false,

		// para critical usamos sempre a mesma tag → renotify volta a fazer barulho
		tag: severity === 'critical'
			? 'hyperhive-critical'
			: Date.now().toString(),

		renotify: severity === 'critical',

		// vibração depende da severidade
		vibrate: vibratePattern,

		// infos podem ser silenciosas se quiseres; aqui deixo todas com som
		silent: false,
	};

	// Log payload to SW console for debugging (open DevTools -> Console -> ServiceWorker)
	console.log('[sw] push received, severity=', severity, 'data=', data, 'using icon=', iconUrl, 'badge=', badgeUrl);

	// Try to fetch the icon so we can see if it's reachable (helps debug 404/CORS issues
	// or other server problems). We still show the notification even if fetch fails.
	const promise = fetch(iconUrl, { method: 'GET' })
		.then(function (resp) {
			if (!resp.ok) throw new Error('icon fetch status=' + resp.status);
			return resp.blob();
		})
		.catch(function (err) {
			console.error('[sw] failed to fetch icon', err);
			// continue and show notification (browser will try to load icon URL too)
		})
		.then(function () {
			return self.registration.showNotification(title, options);
		});

	event.waitUntil(promise);
});

self.addEventListener('notificationclick', function (event) {
	event.notification.close();
	const urlPath = (event.notification.data && event.notification.data.url) || '/';
	// Build full URL dynamically so we don't hardcode domains
	const fullUrl = self.location.origin + urlPath;

	event.waitUntil(clients.openWindow(fullUrl));
});
