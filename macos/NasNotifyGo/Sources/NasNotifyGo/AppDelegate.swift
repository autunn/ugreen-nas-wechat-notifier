import Cocoa
import Foundation

extension String {
    var shellQuoted: String {
        "'" + replacingOccurrences(of: "'", with: "'\\''") + "'"
    }
}

final class AppDelegate: NSObject, NSApplicationDelegate {
    private let port = 5080
    private let serviceLabel = "com.autunn.nasnotify-go.service"
    private let appLabel = "com.autunn.nasnotify-go.app"

    private var statusItem: NSStatusItem?
    private let menu = NSMenu()

    private var statusItemTitle = NSMenuItem()
    private var openDashboardItem = NSMenuItem()
    private var startStopItem = NSMenuItem()
    private var restartItem = NSMenuItem()
    private var autoStartItem = NSMenuItem()
    private var openConfigItem = NSMenuItem()
    private var openLogItem = NSMenuItem()
    private var quitItem = NSMenuItem()

    private var timer: Timer?

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

    private var firstPromptURL: URL {
        appSupportURL.appendingPathComponent(".first_autostart_prompted")
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

    private var backendURL: URL {
        resourcesURL.appendingPathComponent("nasnotify-go-app")
    }

    private var serviceRunnerURL: URL {
        resourcesURL.appendingPathComponent("service-runner.sh")
    }

    private var appExecutableURL: URL {
        Bundle.main.executableURL!
    }

    private var dashboardURL: URL {
        URL(string: "http://localhost:\(port)")!
    }

    func applicationDidFinishLaunching(_ notification: Notification) {
        prepareDirectories()
        createMenuBarItem()
        createMenu()
        refreshMenu()

        timer = Timer.scheduledTimer(
            timeInterval: 3,
            target: self,
            selector: #selector(refreshTimer),
            userInfo: nil,
            repeats: true
        )

        startService(openDashboard: !isLoginLaunch)

        if !isLoginLaunch {
            askAutoStartOnce()
        }
    }

    func applicationWillTerminate(_ notification: Notification) {
        timer?.invalidate()
    }

    private func createMenuBarItem() {
        let item = NSStatusBar.system.statusItem(withLength: NSStatusItem.variableLength)
        item.button?.title = "NAS"
        item.button?.toolTip = "NasNotify-Go"
        item.menu = menu
        statusItem = item
    }

    private func createMenu() {
        menu.autoenablesItems = false

        statusItemTitle = NSMenuItem(title: "NasNotify-Go：启动中", action: nil, keyEquivalent: "")
        statusItemTitle.isEnabled = false
        menu.addItem(statusItemTitle)

        menu.addItem(.separator())

        openDashboardItem = NSMenuItem(title: "打开后台", action: #selector(openDashboard), keyEquivalent: "o")
        openDashboardItem.target = self
        openDashboardItem.isEnabled = true
        menu.addItem(openDashboardItem)

        startStopItem = NSMenuItem(title: "启动服务", action: #selector(toggleService), keyEquivalent: "s")
        startStopItem.target = self
        startStopItem.isEnabled = true
        menu.addItem(startStopItem)

        restartItem = NSMenuItem(title: "重启服务", action: #selector(restartService), keyEquivalent: "r")
        restartItem.target = self
        restartItem.isEnabled = true
        menu.addItem(restartItem)

        menu.addItem(.separator())

        autoStartItem = NSMenuItem(title: "开启登录自启动", action: #selector(toggleAutoStart), keyEquivalent: "")
        autoStartItem.target = self
        autoStartItem.isEnabled = true
        menu.addItem(autoStartItem)

        menu.addItem(.separator())

        openConfigItem = NSMenuItem(title: "打开配置目录", action: #selector(openConfigDirectory), keyEquivalent: "")
        openConfigItem.target = self
        openConfigItem.isEnabled = true
        menu.addItem(openConfigItem)

        openLogItem = NSMenuItem(title: "打开日志", action: #selector(openLogFile), keyEquivalent: "")
        openLogItem.target = self
        openLogItem.isEnabled = true
        menu.addItem(openLogItem)

        menu.addItem(.separator())

        quitItem = NSMenuItem(title: "退出菜单栏 App", action: #selector(quitApp), keyEquivalent: "q")
        quitItem.target = self
        quitItem.isEnabled = true
        menu.addItem(quitItem)
    }

    @objc private func refreshTimer() {
        refreshMenu()
    }

    private func refreshMenu() {
        let running = isServiceRunning()
        let autoStart = isAutoStartEnabled()

        statusItem?.button?.title = running ? "NAS ●" : "NAS ○"
        statusItem?.button?.toolTip = running ? "NasNotify-Go 正在运行" : "NasNotify-Go 已停止"

        statusItemTitle.title = running ? "NasNotify-Go：运行中" : "NasNotify-Go：已停止"
        startStopItem.title = running ? "停止服务" : "启动服务"
        restartItem.isEnabled = running
        autoStartItem.title = autoStart ? "关闭登录自启动" : "开启登录自启动"
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
            alert("初始化目录失败", error.localizedDescription)
        }
    }

    private func isServiceRunning() -> Bool {
        run("/usr/sbin/lsof -nP -iTCP:\(port) -sTCP:LISTEN >/dev/null 2>&1").status == 0
    }

    private func isAutoStartEnabled() -> Bool {
        FileManager.default.fileExists(atPath: servicePlistURL.path)
            && FileManager.default.fileExists(atPath: appPlistURL.path)
    }

    private func startService(openDashboard: Bool) {
        DispatchQueue.global(qos: .utility).async {
            let ok = self.startServiceBlocking()

            DispatchQueue.main.async {
                self.refreshMenu()

                if ok {
                    if openDashboard {
                        self.openDashboard()
                    }
                } else {
                    self.alert("NasNotify-Go 启动失败", "请查看日志：\n\(self.logFileURL.path)")
                }
            }
        }
    }

    private func startServiceBlocking() -> Bool {
        prepareDirectories()

        if isServiceRunning() {
            return true
        }

        if FileManager.default.fileExists(atPath: servicePlistURL.path) {
            _ = run("/bin/launchctl bootstrap gui/$(id -u) \(servicePlistURL.path.shellQuoted) >/dev/null 2>&1 || true")
            _ = run("/bin/launchctl kickstart -k gui/$(id -u)/\(serviceLabel) >/dev/null 2>&1 || true")
        } else {
            let command = """
            cd \(appSupportURL.path.shellQuoted) && \
            nohup \(serviceRunnerURL.path.shellQuoted) >> \(logFileURL.path.shellQuoted) 2>&1 & \
            echo $! > \(pidFileURL.path.shellQuoted)
            """

            _ = run(command)
        }

        for _ in 0..<30 {
            if isServiceRunning() {
                return true
            }

            Thread.sleep(forTimeInterval: 1)
        }

        return false
    }

    private func stopService() {
        DispatchQueue.global(qos: .utility).async {
            self.stopServiceBlocking()

            DispatchQueue.main.async {
                self.refreshMenu()
            }
        }
    }

    private func stopServiceBlocking() {
        _ = run("/bin/launchctl bootout gui/$(id -u)/\(serviceLabel) >/dev/null 2>&1 || true")
        _ = run("/bin/launchctl bootout gui/$(id -u) \(servicePlistURL.path.shellQuoted) >/dev/null 2>&1 || true")

        if FileManager.default.fileExists(atPath: pidFileURL.path) {
            let pid = (try? String(contentsOf: pidFileURL, encoding: .utf8))?
                .trimmingCharacters(in: .whitespacesAndNewlines)

            if let pid, !pid.isEmpty {
                _ = run("/bin/kill \(pid) >/dev/null 2>&1 || true")
            }

            try? FileManager.default.removeItem(at: pidFileURL)
        }

        _ = run("/usr/bin/pkill -f nasnotify-go-app >/dev/null 2>&1 || true")
    }

    @objc private func toggleService() {
        if isServiceRunning() {
            stopService()
        } else {
            startService(openDashboard: true)
        }
    }

    @objc private func restartService() {
        DispatchQueue.global(qos: .utility).async {
            self.stopServiceBlocking()
            Thread.sleep(forTimeInterval: 1)
            let ok = self.startServiceBlocking()

            DispatchQueue.main.async {
                self.refreshMenu()

                if ok {
                    self.openDashboard()
                } else {
                    self.alert("NasNotify-Go 重启失败", "请查看日志：\n\(self.logFileURL.path)")
                }
            }
        }
    }

    @objc private func openDashboard() {
        NSWorkspace.shared.open(dashboardURL)
    }

    @objc private func toggleAutoStart() {
        if isAutoStartEnabled() {
            disableAutoStart()
        } else {
            enableAutoStart()
        }
    }

    private func enableAutoStart() {
        prepareDirectories()
        writeServiceLaunchAgent()
        writeAppLaunchAgent()

        _ = run("/bin/launchctl bootout gui/$(id -u)/\(serviceLabel) >/dev/null 2>&1 || true")
        _ = run("/bin/launchctl bootout gui/$(id -u) \(servicePlistURL.path.shellQuoted) >/dev/null 2>&1 || true")
        _ = run("/bin/launchctl bootstrap gui/$(id -u) \(servicePlistURL.path.shellQuoted) >/dev/null 2>&1 || true")
        _ = run("/bin/launchctl kickstart -k gui/$(id -u)/\(serviceLabel) >/dev/null 2>&1 || true")

        _ = run("/bin/launchctl bootout gui/$(id -u)/\(appLabel) >/dev/null 2>&1 || true")
        _ = run("/bin/launchctl bootout gui/$(id -u) \(appPlistURL.path.shellQuoted) >/dev/null 2>&1 || true")
        _ = run("/bin/launchctl bootstrap gui/$(id -u) \(appPlistURL.path.shellQuoted) >/dev/null 2>&1 || true")

        refreshMenu()
        notify("已开启登录自启动")
    }

    private func disableAutoStart() {
        _ = run("/bin/launchctl bootout gui/$(id -u)/\(appLabel) >/dev/null 2>&1 || true")
        _ = run("/bin/launchctl bootout gui/$(id -u)/\(serviceLabel) >/dev/null 2>&1 || true")
        _ = run("/bin/launchctl bootout gui/$(id -u) \(appPlistURL.path.shellQuoted) >/dev/null 2>&1 || true")
        _ = run("/bin/launchctl bootout gui/$(id -u) \(servicePlistURL.path.shellQuoted) >/dev/null 2>&1 || true")

        try? FileManager.default.removeItem(at: appPlistURL)
        try? FileManager.default.removeItem(at: servicePlistURL)

        refreshMenu()
        notify("已关闭登录自启动")
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
            _ = run("/bin/chmod 644 \(servicePlistURL.path.shellQuoted)")
        } catch {
            alert("写入服务自启动失败", error.localizedDescription)
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
            _ = run("/bin/chmod 644 \(appPlistURL.path.shellQuoted)")
        } catch {
            alert("写入菜单栏自启动失败", error.localizedDescription)
        }
    }

    private func askAutoStartOnce() {
        if FileManager.default.fileExists(atPath: firstPromptURL.path) {
            return
        }

        try? "yes".write(to: firstPromptURL, atomically: true, encoding: .utf8)

        let alert = NSAlert()
        alert.messageText = "是否开启登录自启动？"
        alert.informativeText = "开启后，Mac mini 登录后会自动启动 NasNotify-Go 后台服务，并显示菜单栏图标。"
        alert.addButton(withTitle: "开启自启动")
        alert.addButton(withTitle: "暂不启用")

        if alert.runModal() == .alertFirstButtonReturn {
            enableAutoStart()
        }
    }

    @objc private func openConfigDirectory() {
        NSWorkspace.shared.open(appSupportURL)
    }

    @objc private func openLogFile() {
        if FileManager.default.fileExists(atPath: logFileURL.path) {
            NSWorkspace.shared.open(logFileURL)
        } else {
            alert("日志不存在", logFileURL.path)
        }
    }

    @objc private func quitApp() {
        NSApp.terminate(nil)
    }

    private func alert(_ title: String, _ message: String) {
        DispatchQueue.main.async {
            let alert = NSAlert()
            alert.messageText = title
            alert.informativeText = message
            alert.runModal()
        }
    }

    private func notify(_ message: String) {
        let notification = NSUserNotification()
        notification.title = "NasNotify-Go"
        notification.informativeText = message
        NSUserNotificationCenter.default.deliver(notification)
    }

    @discardableResult
    private func run(_ command: String) -> (status: Int32, output: String) {
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
}