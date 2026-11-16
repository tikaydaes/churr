// The iPhone notification service extension (docs/PUSH.md section 8,
// "iPhone"). iOS starts it when a wake-up arrives with mutable-content.
// It reads the event identifier, finds the server by its wake route in
// the list the app shares, fetches the wake-up from that server with the
// app's token, opens a sealed message with the shared epoch keys, and
// replaces the placeholder. Any failure leaves the placeholder ("New
// message", with the relay's badge) as it is.
//
// The logic is in Sources/ChurrNotify, tested on a Mac with `swift test`.

import UserNotifications

final class NotificationService: UNNotificationServiceExtension {
    private var handler: ((UNNotificationContent) -> Void)?
    private var best: UNMutableNotificationContent?
    private var task: URLSessionDataTask?

    override func didReceive(
        _ request: UNNotificationRequest,
        withContentHandler contentHandler: @escaping (UNNotificationContent) -> Void
    ) {
        handler = contentHandler
        let content = (request.content.mutableCopy() as? UNMutableNotificationContent) ?? UNMutableNotificationContent()
        best = content
        guard let eventId = request.content.userInfo["e"] as? String,
              let settings = SharedItems.servers(),
              let server = settings.server(forEvent: eventId),
              let token = SharedItems.accessToken(for: server),
              let req = WakeFetch.request(server: server, token: token, eventId: eventId)
        else {
            return finish()
        }
        task = URLSession.shared.dataTask(with: req) { [weak self] data, response, _ in
            guard let self else { return }
            // The app is locked on this phone (the lock): the server refused
            // the fetch, and the notification says only that there is one.
            if (response as? HTTPURLResponse)?.statusCode == 401, let data,
               String(decoding: data, as: UTF8.self).contains("\"session_locked\"") {
                content.title = "Churr"
                content.subtitle = ""
                content.body = NSLocalizedString("WAKE_LOCKED", comment: "")
                content.badge = nil
                content.threadIdentifier = "churr-locked"
                return self.finish()
            }
            guard let data, (response as? HTTPURLResponse)?.statusCode == 200,
                  let d = drawWakeUp(data, server: server, settings: settings, words: BundleWords(),
                                     open: { SharedItems.openSealed($0, server: server.id) })
            else { return self.finish() }
            self.apply(d, to: content)
            self.finish()
        }
        task?.resume()
    }

    private func apply(_ d: WakeDrawing, to c: UNMutableNotificationContent) {
        if !d.title.isEmpty { c.title = d.title }
        c.subtitle = d.subtitle
        c.body = d.body
        c.threadIdentifier = d.threadId
        if let b = d.badge { c.badge = NSNumber(value: b) }
        if let p = d.payload {
            var info = c.userInfo
            info["churr"] = p
            c.userInfo = info
        }
        switch d.kind {
        case .ring:
            c.sound = UNNotificationSound(named: UNNotificationSoundName("churr-ring.wav"))
            c.interruptionLevel = .timeSensitive
        case .missedCall:
            c.sound = nil
        default:
            break
        }
    }

    private func finish() {
        guard let h = handler, let c = best else { return }
        handler = nil
        h(c)
    }

    override func serviceExtensionTimeWillExpire() {
        // Out of time: what there is so far, which is the placeholder
        // unless the fetch already finished.
        task?.cancel()
        finish()
    }
}

/// The wording from this extension's Localizable.strings.
struct BundleWords: Words {
    private func s(_ key: String) -> String {
        NSLocalizedString(key, bundle: .main, comment: "")
    }
    func newMessage() -> String { s("WAKE_FALLBACK") }
    func sentFile() -> String { s("SENT_FILE") }
    func postedForm() -> String { s("POSTED_FORM") }
    func sentCard() -> String { s("SENT_CARD") }
    func inChannel(_ sender: String, _ channel: String) -> String {
        String(format: s("IN_CHANNEL"), sender, channel)
    }
    func inChat(_ sender: String, _ chat: String) -> String { String(format: s("IN_CHAT"), sender, chat) }
    func calling(_ name: String) -> String { String(format: s("CALLING"), name) }
    func callBody() -> String { s("CALL_BODY") }
    func missedCall(_ name: String) -> String { String(format: s("MISSED_CALL"), name) }
    func withMore(_ body: String, _ count: Int) -> String { String(format: s("WITH_MORE"), body, count) }
}
