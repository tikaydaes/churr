import Cocoa
import FlutterMacOS

class MainFlutterWindow: NSWindow {
  override func awakeFromNib() {
    let flutterViewController = FlutterViewController()
    let windowFrame = self.frame
    self.contentViewController = flutterViewController
    self.setFrame(windowFrame, display: true)
    // Desktop windows have a minimum size of 360 by 480
    // (docs/CLIENTS.md section 10).
    self.contentMinSize = NSSize(width: 360, height: 480)
    self.title = "Churr"

    RegisterGeneratedPlugins(registry: flutterViewController)

    // The dock badge: the sum of the servers' badges (docs/PUSH.md 13),
    // set from lib/platform/notifications.dart (`IconBadge`).
    let badge = FlutterMethodChannel(
      name: "churr/badge", binaryMessenger: flutterViewController.engine.binaryMessenger)
    badge.setMethodCallHandler { call, result in
      if call.method == "set", let n = call.arguments as? Int {
        NSApp.dockTile.badgeLabel = n > 0 ? (n > 99_999 ? "99999+" : String(n)) : nil
        result(nil)
      } else {
        result(FlutterMethodNotImplemented)
      }
    }

    super.awakeFromNib()
  }
}
