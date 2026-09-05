// JS Demo — frontend plugin (ESM w WebView).
// Eksport default: { onLoad(api), views: [...] } — patrz docs/plugins.md §7.

let launchCount = null;

export default {
	async onLoad(api) {
		const greeting = (api.settings && api.settings.greeting) || "Cześć z pluginu!";

		// Slot na widoku instancji — przycisk akcji
		api.registerSlot("instance-actions", (ctx) => {
			const btn = document.createElement("button");
			btn.className = "topbar-btn";
			btn.textContent = "JS Demo";
			btn.onclick = async () => {
				const res = await api.ui.send("get-greeting");
				api.notify(res && res.message ? res.message + " (instancja: " + ctx.instance + ")" : greeting);
			};
			return btn;
		});

		// Widok w sidebarze z licznikiem launchy (stan trzymany w backendzie)
		api.registerView({
			id: "js-demo-view",
			label: "JS Demo",
			icon: "ph-lightning",
			render(container) {
				container.innerHTML = "";
				const wrap = document.createElement("div");
				wrap.style.cssText = "padding:16px;display:flex;flex-direction:column;gap:12px;";

				const title = document.createElement("div");
				title.style.cssText = "font-size:18px;font-weight:700;color:var(--color-text);";
				title.textContent = "JS Demo";

				const info = document.createElement("div");
				info.style.cssText = "font-size:13px;color:var(--color-text-dim);";
				info.textContent = greeting;

				const counter = document.createElement("div");
				counter.style.cssText = "font-size:13px;color:var(--color-text);";
				counter.textContent = "Launchy w tej sesji: wczytywanie…";

				const refresh = document.createElement("button");
				refresh.className = "topbar-btn";
				refresh.textContent = "Odśwież statystyki";
				refresh.onclick = async () => {
					const stats = await window.go.main.App.RunPluginCommand("js-demo", "stats");
					launchCount = stats && stats.launches;
					const names = (stats && stats.accounts || []).join(", ");
					counter.textContent = "Launchy w tej sesji: " + launchCount +
						(stats && stats.lastInstance ? "\nOstatnia: " + stats.lastInstance : "") +
						(names ? "\nKonta: " + names : "");
				};

				const reset = document.createElement("button");
				reset.className = "topbar-btn";
				reset.textContent = "Zeruj licznik (wiadomość do backendu)";
				reset.onclick = async () => {
					await api.ui.send("reset-counter");
					refresh.onclick();
				};

				wrap.append(title, info, counter, refresh, reset);
				container.appendChild(wrap);
				refresh.onclick();
			},
		});

		api.notify("JS Demo załadowany");
	},
};
