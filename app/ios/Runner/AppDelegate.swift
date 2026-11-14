import Flutter
import UIKit
import UserNotifications

@main
@objc class AppDelegate: FlutterAppDelegate, FlutterImplicitEngineDelegate {
  /// The app's own channel for push (docs/CLIENTS.md section 3: "the raw
  /// APNs device token on iPhone" and "passing the tapped notification
  /// from the iPhone extension to the app"); lib/platform/push.dart.
  private var push: FlutterMethodChannel?
  /// The icon badge (lib/platform/notifications.dart, `IconBadge`).
  private var badge: FlutterMethodChannel?
  /// A tap that opened the app before Dart was listening.
  private var pendingTap: String?
  private var token: String?

  override func application(
    _ application: UIApplication,
    didFinishLaunchingWithOptions launchOptions: [UIApplication.LaunchOptionsKey: Any]?
  ) -> Bool {
    UNUserNotificationCenter.current().delegate = self
    // The status endpoint on the system's background refresh
    // (docs/PUSH.md section 11): a few times an hour at best.
    application.setMinimumBackgroundFetchInterval(UIApplication.backgroundFetchIntervalMinimum)
    return super.application(application, didFinishLaunchingWithOptions: launchOptions)
  }

  func didInitializeImplicitFlutterEngine(_ engineBridge: FlutterImplicitEngineBridge) {
    GeneratedPluginRegistrant.register(with: engineBridge.pluginRegistry)
    let messenger = engineBridge.pluginRegistry.registrar(forPlugin: "ChurrPush")!.messenger()
    let push = FlutterMethodChannel(name: "churr/push", binaryMessenger: messenger)
    push.setMethodCallHandler { [weak self] call, result in
      guard let self else { return result(nil) }
      switch call.method {
      case "register":
        UIApplication.shared.registerForRemoteNotifications()
        if let t = self.token { push.invokeMethod("token", arguments: t) }
        result(self.pendingTap)
        self.pendingTap = nil
      default:
        result(FlutterMethodNotImplemented)
      }
    }
    self.push = push
    let badge = FlutterMethodChannel(name: "churr/badge", binaryMessenger: messenger)
    badge.setMethodCallHandler { call, result in
      guard call.method == "set", let n = call.arguments as? Int else {
        return result(FlutterMethodNotImplemented)
      }
      if #available(iOS 16.0, *) {
        UNUserNotificationCenter.current().setBadgeCount(n)
      } else {
        UIApplication.shared.applicationIconBadgeNumber = n
      }
      result(nil)
    }
    self.badge = badge
  }

  // MARK: The APNs token: registered on each server as push kind `apns`.

  override func application(
    _ application: UIApplication,
    didRegisterForRemoteNotificationsWithDeviceToken deviceToken: Data
  ) {
    let hex = deviceToken.map { String(format: "%02x", $0) }.joined()
    token = hex
    push?.invokeMethod("token", arguments: hex)
    super.application(application, didRegisterForRemoteNotificationsWithDeviceToken: deviceToken)
  }

  override func application(
    _ application: UIApplication,
    didFailToRegisterForRemoteNotificationsWithError error: Error
  ) {
    // No token (the simulator, no push entitlement): the device stays
    // `none` and the status endpoint keeps the badge.
    NSLog("Churr: no APNs token: \(error.localizedDescription)")
  }

  // MARK: Wake-ups: shown by the extension; a tap opens the message.

  override func userNotificationCenter(
    _ center: UNUserNotificationCenter,
    didReceive response: UNNotificationResponse,
    withCompletionHandler completionHandler: @escaping () -> Void
  ) {
    let info = response.notification.request.content.userInfo
    if response.notification.request.trigger is UNPushNotificationTrigger {
      let payload = info["churr"] as? String
      if let push, payload != nil {
        push.invokeMethod("tapped", arguments: ["churr": payload!])
      } else {
        pendingTap = payload
      }
      completionHandler()
      return
    }
    super.userNotificationCenter(center, didReceive: response, withCompletionHandler: completionHandler)
  }

  override func userNotificationCenter(
    _ center: UNUserNotificationCenter,
    willPresent notification: UNNotification,
    withCompletionHandler completionHandler: @escaping (UNNotificationPresentationOptions) -> Void
  ) {
    if notification.request.trigger is UNPushNotificationTrigger {
      // In front, the app is connected and shows its own; the wake-up
      // still sets the badge and tells Dart to ask for the totals.
      let info = notification.request.content.userInfo
      push?.invokeMethod("wake", arguments: ["e": info["e"] as? String ?? "", "b": info["aps"].flatMap { ($0 as? [String: Any])?["badge"] } ?? NSNull()])
      completionHandler([.badge])
      return
    }
    super.userNotificationCenter(center, willPresent: notification, withCompletionHandler: completionHandler)
  }

  // MARK: Background refresh: the status endpoint, and "New activity".

  override func application(
    _ application: UIApplication,
    performFetchWithCompletionHandler completionHandler: @escaping (UIBackgroundFetchResult) -> Void
  ) {
    guard let push else { return completionHandler(.noData) }
    var done = false
    let finish: (UIBackgroundFetchResult) -> Void = { r in
      if !done { done = true; completionHandler(r) }
    }
    DispatchQueue.main.asyncAfter(deadline: .now() + 25) { finish(.failed) }
    push.invokeMethod("backgroundRefresh", arguments: nil) { _ in finish(.newData) }
  }
}
