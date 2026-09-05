// JS Demo — backend plugin (goja). Uruchamiany po wlaczeniu pluginu.
// API: global `launcher` (patrz docs/plugins.md §6).

let launches = 0;
let lastInstance = null;

// 1) Eventy z EventBusa (wymaga permissions: events:*)
launcher.on("launch:start", (e) => {
	launches++;
	lastInstance = e.instance;
	launcher.storage.set({ key: "lastLaunch", value: e });
	launcher.log("js-demo: launch started: " + e.instance + " (" + e.version + ")");
});

launcher.on("launch:exit", (e) => {
	launcher.log("js-demo: " + e.instance + " exited with code " + e.exitCode);
});

// 2) Komenda wywolywana z UI przez GetPluginCommands / RunPluginCommand
launcher.registerCommand("stats", () => {
	let accounts = [];
	try {
		accounts = launcher.accounts.list() || [];
	} catch (err) {
		// brak permission accounts:read -> pomin
	}
	return {
		launches: launches,
		lastInstance: lastInstance,
		lastLaunch: launcher.storage.get("lastLaunch"),
		accounts: accounts.map((a) => a.name),
	};
});

// 3) Messaging frontend -> backend (api.ui.send(name, data))
launcher.ui.on("reset-counter", () => {
	launches = 0;
	launcher.log("js-demo: counter reset from frontend");
	return { launches: 0 };
});

launcher.ui.on("get-greeting", () => {
	return { message: "hello from goja backend" };
});

launcher.log("js-demo backend started");
