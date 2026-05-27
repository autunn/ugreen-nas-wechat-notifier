import Cocoa

let app = NSApplication.shared

// Set a runtime application icon when running from the generated .app bundle.
// The Finder / Launchpad icon is controlled by Info.plist + AppIcon.icns.
if let resourceURL = Bundle.main.resourceURL?.appendingPathComponent("AppIcon.png"),
   let icon = NSImage(contentsOf: resourceURL) {
    app.applicationIconImage = icon
}

let delegate = AppDelegate()
app.delegate = delegate

// Menu bar app: hide Dock icon by default.
app.setActivationPolicy(.accessory)

app.run()
