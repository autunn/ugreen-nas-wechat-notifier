import Cocoa
import Foundation

extension String {
    var shellQuoted: String {
        return "'" + self.replacingOccurrences(of: "'", with: "'\\''") + "'"
    }
}

final class AppDelegate: NSObject, NSApplicationDelegate, NSMenuDelegate {
    private let port = 5080

    private let serviceLabel = "com.autunn.nasnotify-go.service"
    private let appLabel = "com.autunn.nasnotify-go.app"

    private var statusItem: NSStatusItem!
    private var statusMenu = NSMenu()

    private var isLoginLaunch: Bool {
        CommandLine.arguments.contains("--login-item")
    }

    private var appSupportURL: URL {
        FileManager.default.homeDirectoryForCurrentUser
            .appendingPathComponent("Library/Application Support/NasNotify-Go", isDirectory: true)
    }

    private var configURL: URL {
        appSupportURL.appendingPathComponent("config", isDirectory: true)
    }

    private var dataURL: URL {
        appSupportURL.appendingPathComponent("data", isDirectory: true)
    }

    private var logDirURL: URL {
        FileManager.default.homeDirectoryForCurrentUser
            .appendingPathComponent("Library/Logs", isDirectory: true)
    }

    private var logFileURL: URL {
        logDirURL.appendingPathComponent("NasNotify-Go.log")
    }

    private var pidFileURL: URL {
        appSupportURL.appendingPathComponent("nasnotify-go.pid")
    }

    private var promptFileURL: URL {
        appSupportURL.appendingPathComponent(".autostart_prompted")
    }

    private var launchAgentsURL: URL {
        FileManager.default.homeDirectoryForCurrentUser
            .appendingPathComponent("Library/LaunchAgents", isDirectory: true)
    }

    private var servicePlistURL: URL {
        launchAgentsURL.appendingPathComponent("\(serviceLabel).plist")
    }

    private var appPlistURL: URL {
        launchAgentsURL.appendingPathComponent("\(appLabel).plist")
    }

    private var resourcesURL: URL {
        Bundle.main.resourceURL!
    }

    private var backendBinaryURL: URL {
        resourcesURL.appendingPathComponent("nasnotify-go-app")
    }

    private var serviceRunnerURL: URL {
        resourcesURL.appendingPathComponent("service-runner.sh")
    }

    private var appExecutableURL: URL {
        Bundle.main.executableURL!
    }

    private var webURL: URL {
        URL(string: "http://localhost:\(port)")!
    }

    func applicationDidFinishLaunching(_ notification: Notification) {
        NSApp.setActivationPolicy(.accessory)

        setupStatusItem()
        prepareDirectories()

        startService(openWeb: !isLoginLaunch)

        if !isLoginLaunch {
            askAutoStartOnFirstLaunch()
        }

        showNotification("NasNotify-Go 已在后台运行")
    }

    func applicationShouldHandleReopen(_ sender: NSApplication, hasVisibleWindows flag: Bool) -> Bool {
        openWebConsole()
        return false
    }

    private func setupStatusItem() {
        statusItem = NSStatusBar.system.statusItem(withLength: NSStatusItem.variableLength)

        if let button = statusItem.button {
            button.title = "NAS"
            button.toolTip = "NasNotify-Go"
        }

        statusMenu.delegate = self
        statusMenu.autoenablesItems = false

        rebuildMenu()

        statusItem.menu = statusMenu
    }

    func menuNeedsUpdate(_ menu: NSMenu) {
        rebuildMenu()
    }

    private func rebuildMenu() {
        statusMenu.removeAllItems()

        let statusTitle = isServiceRunning() ? "NasNotify-Go：运行中" : "NasNotify-Go：已停止"
        let statusMenuItem = NSMenuItem(title: statusTitle, action: nil, keyEquivalent: "")
        statusMenuItem.isEnabled = false
        statusMenu.addItem(statusMenuItem)

        statusMenu.addItem(NSMenuItem.separator())

        let openItem = NSMenuItem(title: "打开后台", action: #selector(openWebConsoleAction), keyEquivalent: "o")
        openItem.target = self
        openItem.isEnabled = true
        statusMenu.addItem(openItem)

        if isServiceRunning() {
            let stopItem = NSMenuItem(title: "停止服务", action: #selector(stopServiceAction), keyEquivalent: "s")
            stopItem.target = self
            stopItem.isEnabled = true
            statusMenu.addItem(stopItem)

            let restartItem = NSMenuItem(title: "重启服务", action: #selector(restartServiceAction), keyEquivalent: "r")
            restartItem.target = self
            restartItem.isEnabled = true
            statusMenu.addItem(restartItem)
        } else {
            let startItem = NSMenuItem(title: "启动服务", action: #selector(startServiceAction), keyEquivalent: "s")
            startItem.target = self
            startItem.isEnabled = true
            statusMenu.addItem(startItem)
        }

        statusMenu.addItem(NSMenuItem.separator())

        let autoStartTitle = isAutoStartEnabled() ? "关闭登录自启动" : "开启登录自启动"
        let autoStartItem = NSMenuItem(title: autoStartTitle, action: #selector(toggleAutoStartAction), keyEquivalent: "")
        autoStartItem.target = self
        autoStartItem.isEnabled = true
        statusMenu.addItem(autoStartItem)

        statusMenu.addItem(NSMenuItem.separator())

        let configItem = NSMenuItem(title: "打开配置目录", action: #selector(openConfigFolderAction), keyEquivalent: "")
        configItem.target = self
        configItem.isEnabled = true
        statusMenu.addItem(configItem)

        let logItem = NSMenuItem(title: "打开日志", action: #selector(openLogAction), keyEquivalent: "")
        logItem.target = self
        logItem.isEnabled = true
        statusMenu.addItem(logItem)

        statusMenu.addItem(NSMenuItem.separator())

        let quitItem = NSMenuItem(title: "退出菜单栏 App", action: #selector(quitAppAction), keyEquivalent: "q")
        quitItem.target = self
        quitItem.isEnabled = true
        statusMenu.addItem(quitItem)
    }

    private func prepareDirectories() {
        let fm = FileManager.default

        do {
            try fm.createDirectory(at: appSupportURL, withIntermediateDirectories: true)
            try fm.createDirectory(at: configURL, withIntermediateDirectories: true)
            try fm.createDirectory(at: dataURL, withIntermediateDirectories: true)
            try fm.createDirectory(at: logDirURL, withIntermediateDirectories: true)
            try fm.createDirectory(at: launchAgentsURL, withIntermediateDirectories: true)

            let bundledTemplates = resourcesURL.appendingPathComponent("templates")
            let targetTemplates = appSupportURL.appendingPathComponent("templates")

            if fm.fileExists(atPath: bundledTemplates.path) {
                if fm.fileExists(atPath: targetTemplates.path) {
                    try fm.removeItem(at: targetTemplates)
                }

                try fm.copyItem(at: bundledTemplates, to: targetTemplates)
            }
        } catch {
            showAlert(title: "初始化目录失败", message: error.localizedDescription)
        }
    }

    private func isServiceRunning() -> Bool {
        let result = runShell("/usr/sbin/lsof -nP -iTCP:\(port) -sTCP:LISTEN >/dev/null 2>&1")
        return result.status == 0
    }

    private func isAutoStartEnabled() -> Bool {
        FileManager.default.fileExists(atPath: servicePlistURL.path)
            && FileManager.default.fileExists(atPath: appPlistURL.path)
    }

    private func startService(openWeb: Bool) {
        prepareDirectories()

        if isServiceRunning() {
            if openWeb {
                openWebConsole()
            }

            rebuildMenu()
            return
        }

        if FileManager.default.fileExists(atPath: servicePlistURL.path) {
            _ = runShell("/bin/launchctl bootstrap gui/$(id -u) \(servicePlistURL.path.shellQuoted) >/dev/null 2>&1 || true")
            _ = runShell("/bin/launchctl kickstart -k gui/$(id -u)/\(serviceLabel) >/dev/null 2>&1 || true")
        } else {
            let command = """
            cd \(appSupportURL.path.shellQuoted) && \
            nohup \(serviceRunnerURL.path.shellQuoted) >> \(logFileURL.path.shellQuoted) 2>&1 & \
            echo $! > \(pidFileURL.path.shellQuoted)
            """

            _ = runShell(command)
        }

        for _ in 0..<30 {
            if isServiceRunning() {
                if openWeb {
                    openWebConsole()
                }

                rebuildMenu()
                return
            }

            Thread.sleep(forTimeInterval: 1)
        }

        showAlert(title: "NasNotify-Go 启动失败", message: "请查看日志：\n\(logFileURL.path)")
        rebuildMenu()
    }

    private func stopService() {
        _ = runShell("/bin/launchctl bootout gui/$(id -u)/\(serviceLabel) >/dev/null 2>&1 || true")
        _ = runShell("/bin/launchctl bootout gui/$(id -u) \(servicePlistURL.path.shellQuoted) >/dev/null 2>&1 || true")

        if FileManager.default.fileExists(atPath: pidFileURL.path) {
            let pidText = (try? String(contentsOf: pidFileURL, encoding: .utf8))?
                .trimmingCharacters(in: .whitespacesAndNewlines)

            if let pidText, !pidText.isEmpty {
                _ = runShell("/bin/kill \(pidText) >/dev/null 2>&1 || true")
            }

            try? FileManager.default.removeItem(at: pidFileURL)
        }

        _ = runShell("/usr/bin/pkill -f nasnotify-go-app >/dev/null 2>&1 || true")

        showNotification("后台服务已停止")
        rebuildMenu()
    }

    private func restartService() {
        stopService()
        Thread.sleep(forTimeInterval: 1)
        startService(openWeb: true)
    }

    private func enableAutoStart() {
        prepareDirectories()
        writeServiceLaunchAgent()
        writeAppLaunchAgent()

        _ = runShell("/bin/launchctl bootout gui/$(id -u)/\(serviceLabel) >/dev/null 2>&1 || true")
        _ = runShell("/bin/launchctl bootout gui/$(id -u) \(servicePlistURL.path.shellQuoted) >/dev/null 2>&1 || true")
        _ = runShell("/bin/launchctl bootstrap gui/$(id -u) \(servicePlistURL.path.shellQuoted) >/dev/null 2>&1 || true")
        _ = runShell("/bin/launchctl kickstart -k gui/$(id -u)/\(serviceLabel) >/dev/null 2>&1 || true")

        _ = runShell("/bin/launchctl bootout gui/$(id -u)/\(appLabel) >/dev/null 2>&1 || true")
        _ = runShell("/bin/launchctl bootout gui/$(id -u) \(appPlistURL.path.shellQuoted) >/dev/null 2>&1 || true")
        _ = runShell("/bin/launchctl bootstrap gui/$(id -u) \(appPlistURL.path.shellQuoted) >/dev/null 2>&1 || true")

        showNotification("已开启登录自启动")
        rebuildMenu()
    }

    private func disableAutoStart() {
        _ = runShell("/bin/launchctl bootout gui/$(id -u)/\(appLabel) >/dev/null 2>&1 || true")
        _ = runShell("/bin/launchctl bootout gui/$(id -u)/\(serviceLabel) >/dev/null 2>&1 || true")
        _ = runShell("/bin/launchctl bootout gui/$(id -u) \(appPlistURL.path.shellQuoted) >/dev/null 2>&1 || true")
        _ = runShell("/bin/launchctl bootout gui/$(id -u) \(servicePlistURL.path.shellQuoted) >/dev/null 2>&1 || true")

        try? FileManager.default.removeItem(at: appPlistURL)
        try? FileManager.default.removeItem(at: servicePlistURL)

        showNotification("已关闭登录自启动")
        rebuildMenu()
    }

    private func writeServiceLaunchAgent() {
        let plist = """
        <?xml version="1.0" encoding="UTF-8"?>
        <!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN"
          "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
        <plist version="1.0">
        <dict>
          <key>Label</key>
          <string>\(serviceLabel)</string>

          <key>ProgramArguments</key>
          <array>
            <string>\(serviceRunnerURL.path)</string>
          </array>

          <key>WorkingDirectory</key>
          <string>\(appSupportURL.path)</string>

          <key>RunAtLoad</key>
          <true/>

          <key>KeepAlive</key>
          <true/>

          <key>StandardOutPath</key>
          <string>\(logDirURL.appendingPathComponent("NasNotify-Go.launchd.out.log").path)</string>

          <key>StandardErrorPath</key>
          <string>\(logDirURL.appendingPathComponent("NasNotify-Go.launchd.err.log").path)</string>

          <key>ProcessType</key>
          <string>Background</string>
        </dict>
        </plist>
        """

        do {
            try plist.write(to: servicePlistURL, atomically: true, encoding: .utf8)
            _ = runShell("/bin/chmod 644 \(servicePlistURL.path.shellQuoted)")
        } catch {
            showAlert(title: "写入服务自启动失败", message: error.localizedDescription)
        }
    }

    private func writeAppLaunchAgent() {
        let plist = """
        <?xml version="1.0" encoding="UTF-8"?>
        <!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN"
          "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
        <plist version="1.0">
        <dict>
          <key>Label</key>
          <string>\(appLabel)</string>

          <key>ProgramArguments</key>
          <array>
            <string>\(appExecutableURL.path)</string>
            <string>--login-item</string>
          </array>

          <key>RunAtLoad</key>
          <true/>

          <key>KeepAlive</key>
          <false/>

          <key>ProcessType</key>
          <string>Interactive</string>
        </dict>
        </plist>
        """

        do {
            try plist.write(to: appPlistURL, atomically: true, encoding: .utf8)
            _ = runShell("/bin/chmod 644 \(appPlistURL.path.shellQuoted)")
        } catch {
            showAlert(title: "写入菜单栏自启动失败", message: error.localizedDescription)
        }
    }

    private func askAutoStartOnFirstLaunch() {
        if FileManager.default.fileExists(atPath: promptFileURL.path) {
            return
        }

        try? "prompted".write(to: promptFileURL, atomically: true, encoding: .utf8)

        let alert = NSAlert()
        alert.messageText = "是否开启登录自启动？"
        alert.informativeText = "开启后，Mac mini 登录后会自动启动 NasNotify-Go 后台服务，并显示菜单栏图标。"
        alert.addButton(withTitle: "开启自启动")
        alert.addButton(withTitle: "暂不启用")

        let response = alert.runModal()

        if response == .alertFirstButtonReturn {
            enableAutoStart()
        }
    }

    private func openWebConsole() {
        NSWorkspace.shared.open(webURL)
    }

    private func showAlert(title: String, message: String) {
        DispatchQueue.main.async {
            let alert = NSAlert()
            alert.messageText = title
            alert.informativeText = message
            alert.runModal()
        }
    }

    private func showNotification(_ message: String) {
        let notification = NSUserNotification()
        notification.title = "NasNotify-Go"
        notification.informativeText = message
        NSUserNotificationCenter.default.deliver(notification)
    }

    private func runShell(_ command: String) -> (status: Int32, output: String) {
        let process = Process()
        process.executableURL = URL(fileURLWithPath: "/bin/bash")
        process.arguments = ["-lc", command]

        let pipe = Pipe()
        process.standardOutput = pipe
        process.standardError = pipe

        do {
            try process.run()
            process.waitUntilExit()

            let data = pipe.fileHandleForReading.readDataToEndOfFile()
            let output = String(data: data, encoding: .utf8) ?? ""

            return (process.terminationStatus, output)
        } catch {
            return (1, error.localizedDescription)
        }
    }

    @objc private func openWebConsoleAction() {
        openWebConsole()
        rebuildMenu()
    }

    @objc private func startServiceAction() {
        startService(openWeb: true)
    }

    @objc private func stopServiceAction() {
        stopService()
    }

    @objc private func restartServiceAction() {
        restartService()
    }

    @objc private func toggleAutoStartAction() {
        if isAutoStartEnabled() {
            disableAutoStart()
        } else {
            enableAutoStart()
        }
    }

    @objc private func openConfigFolderAction() {
        NSWorkspace.shared.open(appSupportURL)
    }

    @objc private func openLogAction() {
        if FileManager.default.fileExists(atPath: logFileURL.path) {
            NSWorkspace.shared.open(logFileURL)
        } else {
            showAlert(title: "日志不存在", message: logFileURL.path)
        }
    }

    @objc private func quitAppAction() {
        NSApp.terminate(nil)
    }
}

let app = NSApplication.shared
let delegate = AppDelegate()
app.delegate = delegate
app.run()