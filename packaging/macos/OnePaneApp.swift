import Cocoa
import WebKit

private let controlURL = URL(string: "http://127.0.0.1:18181")!

final class AppDelegate: NSObject, NSApplicationDelegate, NSWindowDelegate {
    private var window: NSWindow!
    private var webView: WKWebView!
    private var backendProcess: Process?
    private var backendLog: FileHandle?
    private var launchError: String?

    func applicationDidFinishLaunching(_ notification: Notification) {
        configureMenu()
        createWindow()
        NSApp.activate(ignoringOtherApps: true)
        window.makeKeyAndOrderFront(nil)

        do {
            try ensureConfiguration()
            showLoading("Starting OnePane…")
            startBackendIfNeeded()
            waitForBackend(attempt: 0)
        } catch {
            launchError = error.localizedDescription
            showFailure(error.localizedDescription)
        }
    }

    func applicationShouldTerminateAfterLastWindowClosed(_ sender: NSApplication) -> Bool {
        return true
    }

    func applicationWillTerminate(_ notification: Notification) {
        if let process = backendProcess, process.isRunning {
            process.terminate()
        }
        try? backendLog?.close()
    }

    private func configureMenu() {
        let mainMenu = NSMenu()
        let appMenuItem = NSMenuItem()
        mainMenu.addItem(appMenuItem)
        let appMenu = NSMenu()
        appMenu.addItem(withTitle: "About OnePane", action: #selector(NSApplication.orderFrontStandardAboutPanel(_:)), keyEquivalent: "")
        appMenu.addItem(NSMenuItem.separator())
        appMenu.addItem(withTitle: "Quit OnePane", action: #selector(NSApplication.terminate(_:)), keyEquivalent: "q")
        appMenuItem.submenu = appMenu
        NSApp.mainMenu = mainMenu
    }

    private func createWindow() {
        let config = WKWebViewConfiguration()
        config.websiteDataStore = .default()
        webView = WKWebView(frame: .zero, configuration: config)
        webView.setValue(false, forKey: "drawsBackground")

        window = NSWindow(
            contentRect: NSRect(x: 0, y: 0, width: 1380, height: 880),
            styleMask: [.titled, .closable, .miniaturizable, .resizable, .fullSizeContentView],
            backing: .buffered,
            defer: false
        )
        window.title = "OnePane"
        window.titlebarAppearsTransparent = true
        window.center()
        window.minSize = NSSize(width: 920, height: 620)
        window.contentView = webView
        window.delegate = self
    }

    private func appSupportDirectory() throws -> URL {
        let fm = FileManager.default
        let root = try fm.url(for: .applicationSupportDirectory, in: .userDomainMask, appropriateFor: nil, create: true)
            .appendingPathComponent("OnePane", isDirectory: true)
        try fm.createDirectory(at: root, withIntermediateDirectories: true)
        return root
    }

    private func yamlQuoted(_ value: String) -> String {
        let escaped = value.replacingOccurrences(of: "\\", with: "\\\\").replacingOccurrences(of: "\"", with: "\\\"")
        return "\"\(escaped)\""
    }

    private func ensureConfiguration() throws {
        let fm = FileManager.default
        let root = try appSupportDirectory()
        let data = root.appendingPathComponent("data", isDirectory: true)
        let models = root.appendingPathComponent("models", isDirectory: true)
        let logs = root.appendingPathComponent("logs", isDirectory: true)
        try fm.createDirectory(at: data, withIntermediateDirectories: true)
        try fm.createDirectory(at: models, withIntermediateDirectories: true)
        try fm.createDirectory(at: logs, withIntermediateDirectories: true)

        let config = root.appendingPathComponent("config.yaml")
        if !fm.fileExists(atPath: config.path) {
            let text = """
            server:
              listen: "127.0.0.1:18181"
              preview_listen: "127.0.0.1:18182"
            storage:
              data_dir: \(yamlQuoted(data.path))
            logging:
              level: "info"
            local_ai:
              model_pool_path: \(yamlQuoted(models.path))
              idle_unload_minutes: 15
              residency_headroom_pct: 10
            node_federation:
              enabled: true
              listen: "0.0.0.0:18443"
              discovery_enabled: true
              discovery_multicast: "239.255.77.77:47777"
              heartbeat_seconds: 10
              stale_seconds: 35
            """
            try text.write(to: config, atomically: true, encoding: .utf8)
        }
    }

    private func backendHealthy() -> Bool {
        let semaphore = DispatchSemaphore(value: 0)
        var healthy = false
        var request = URLRequest(url: controlURL.appendingPathComponent("v1/health"))
        request.timeoutInterval = 1.0
        URLSession.shared.dataTask(with: request) { _, response, _ in
            if let http = response as? HTTPURLResponse, (200..<500).contains(http.statusCode) {
                healthy = true
            }
            semaphore.signal()
        }.resume()
        _ = semaphore.wait(timeout: .now() + 1.5)
        return healthy
    }

    private func startBackendIfNeeded() {
        DispatchQueue.global(qos: .userInitiated).async { [weak self] in
            guard let self else { return }
            if self.backendHealthy() { return }
            do {
                let root = try self.appSupportDirectory()
                let config = root.appendingPathComponent("config.yaml")
                let logs = root.appendingPathComponent("logs", isDirectory: true)
                try FileManager.default.createDirectory(at: logs, withIntermediateDirectories: true)
                let logURL = logs.appendingPathComponent("onepane.log")
                if !FileManager.default.fileExists(atPath: logURL.path) {
                    FileManager.default.createFile(atPath: logURL.path, contents: nil)
                }
                let log = try FileHandle(forWritingTo: logURL)
                try log.seekToEnd()

                guard let backend = Bundle.main.executableURL?.deletingLastPathComponent().appendingPathComponent("OnePane.Backend") else {
                    throw NSError(domain: "OnePane", code: 1, userInfo: [NSLocalizedDescriptionKey: "Bundled OnePane backend was not found."])
                }
                let process = Process()
                process.executableURL = backend
                process.arguments = ["-config", config.path]
                process.currentDirectoryURL = backend.deletingLastPathComponent()
                process.standardOutput = log
                process.standardError = log
                try process.run()
                self.backendLog = log
                self.backendProcess = process
            } catch {
                DispatchQueue.main.async {
                    self.launchError = error.localizedDescription
                    self.showFailure(error.localizedDescription)
                }
            }
        }
    }

    private func waitForBackend(attempt: Int) {
        if let launchError {
            showFailure(launchError)
            return
        }
        if attempt >= 120 {
            showFailure("The OnePane backend did not become healthy. Check ~/Library/Application Support/OnePane/logs/onepane.log.")
            return
        }
        DispatchQueue.global(qos: .utility).async { [weak self] in
            guard let self else { return }
            let ready = self.backendHealthy()
            DispatchQueue.main.async {
                if ready {
                    self.webView.load(URLRequest(url: controlURL))
                } else {
                    DispatchQueue.main.asyncAfter(deadline: .now() + 0.5) {
                        self.waitForBackend(attempt: attempt + 1)
                    }
                }
            }
        }
    }

    private func showLoading(_ message: String) {
        let html = """
        <!doctype html><meta charset="utf-8"><style>
        html,body{height:100%;margin:0;background:#07111b;color:#e8f0f7;font-family:-apple-system,BlinkMacSystemFont,sans-serif}
        body{display:grid;place-items:center}.card{text-align:center}.dot{width:11px;height:11px;border-radius:50%;background:#3aa0ff;display:inline-block;margin-right:10px;box-shadow:0 0 22px #3aa0ff}
        </style><div class="card"><span class="dot"></span>\(message)</div>
        """
        webView.loadHTMLString(html, baseURL: nil)
    }

    private func showFailure(_ message: String) {
        let escaped = message
            .replacingOccurrences(of: "&", with: "&amp;")
            .replacingOccurrences(of: "<", with: "&lt;")
            .replacingOccurrences(of: ">", with: "&gt;")
        webView.loadHTMLString("<body style='font:15px -apple-system;padding:40px;background:#111827;color:#f8fafc'><h2>OnePane could not start</h2><p>\(escaped)</p></body>", baseURL: nil)
    }
}

let app = NSApplication.shared
let delegate = AppDelegate()
app.delegate = delegate
app.setActivationPolicy(.regular)
app.run()
