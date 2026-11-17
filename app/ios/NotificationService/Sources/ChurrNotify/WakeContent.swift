// What the notification extension draws for one wake-up (docs/PUSH.md
// sections 8 and 9): the fetch's answer turned into a title, a body, a
// thread identifier, a badge and what opening it should show. The same
// rules as lib/core/notify/wake.dart and lib/platform/notify_wording.dart
// on the Dart side.

import Foundation

/// One server from the shared list (app/docs/NOTIFY-SHARED.md,
/// `churr.notify.servers`).
public struct SharedServer: Equatable {
    public let id: String
    public let address: URL
    public let name: String
    public let route: String
    public let tokenKey: String
}

/// The whole shared list and the settings that go with it.
public struct SharedServers: Equatable {
    public let servers: [SharedServer]
    public let showText: Bool
    public let multi: Bool

    public init?(json: Data) {
        guard let j = try? JSONSerialization.jsonObject(with: json) as? [String: Any],
              let list = j["servers"] as? [[String: Any]] else { return nil }
        servers = list.compactMap { s in
            guard let id = s["id"] as? String,
                  let a = (s["address"] as? String).flatMap(URL.init(string:)),
                  let r = s["route"] as? String else { return nil }
            return SharedServer(id: id, address: a, name: s["name"] as? String ?? "",
                                route: r, tokenKey: s["token_key"] as? String ?? "churr.tokens.\(id)")
        }
        showText = j["show_text"] as? Bool ?? true
        multi = j["multi"] as? Bool ?? false
    }

    /// The server a wake-up came from: its event identifier begins with
    /// the server's wake route and a dot.
    public func server(forEvent eventId: String) -> SharedServer? {
        guard let dot = eventId.firstIndex(of: ".") else { return nil }
        let route = String(eventId[..<dot])
        return servers.first { $0.route == route }
    }
}

/// The wording, from the extension's Localizable.strings.
public protocol Words {
    func newMessage() -> String
    func sentFile() -> String
    func postedForm() -> String
    func sentCard() -> String
    func inChannel(_ sender: String, _ channel: String) -> String
    func inChat(_ sender: String, _ chat: String) -> String
    func calling(_ name: String) -> String
    func callBody() -> String
    func missedCall(_ name: String) -> String
    func requestBody() -> String
    func withMore(_ body: String, _ count: Int) -> String
}

/// What to put on screen.
public struct WakeDrawing: Equatable {
    public enum Kind: Equatable { case message, ring, missedCall, placeholder }
    public var kind: Kind
    public var title: String
    public var subtitle: String
    public var body: String
    public var threadId: String
    public var badge: Int?
    /// The tap payload the app reads (`TapTarget` in
    /// lib/state/notify_center.dart), under the user-info key "churr".
    public var payload: String?
}

/// Reads the `fetchWakeUp` answer and draws it. [open] opens a sealed
/// message for the messages of a sealed chat; nil shows the placeholder.
public func drawWakeUp(
    _ json: Data,
    server: SharedServer,
    settings: SharedServers,
    words: Words,
    open: ((_ message: [String: Any]) -> String?)? = nil
) -> WakeDrawing? {
    guard let root = try? JSONSerialization.jsonObject(with: json) as? [String: Any],
          let w = root["wake_up"] as? [String: Any],
          let conv = w["conversation"] as? [String: Any],
          let convId = conv["id"] as? String else { return nil }
    let badge = (w["badge"] as? NSNumber)?.intValue
    let subtitle = settings.multi ? server.name : ""
    func payload(message: String? = nil, parent: String? = nil, call: String? = nil) -> String {
        var p: [String: String] = ["s": server.id, "c": convId]
        if let message { p["m"] = message }
        if let parent { p["p"] = parent }
        if let call { p["call"] = call }
        let d = (try? JSONSerialization.data(withJSONObject: p, options: [.sortedKeys])) ?? Data()
        return String(decoding: d, as: UTF8.self)
    }
    if (w["kind"] as? String) == "ring", let call = w["call"] as? [String: Any] {
        let name = call["from_name"] as? String ?? ""
        let ringing = call["ringing"] as? Bool ?? false
        return WakeDrawing(
            kind: ringing ? .ring : .missedCall,
            title: ringing ? words.calling(name) : words.missedCall(name),
            subtitle: subtitle, body: ringing ? words.callBody() : "",
            threadId: convId, badge: badge,
            payload: payload(call: ringing ? call["id"] as? String : nil))
    }
    if (w["kind"] as? String) == "request" {
        // A chat request: only who is asking (ruling R78). It opens the
        // app, and no conversation; one the request no longer waits on
        // draws the placeholder, which iOS cannot withdraw.
        let req = w["request"] as? [String: Any] ?? [:]
        let name = req["from_name"] as? String ?? ""
        if (w["read"] as? Bool ?? false) || name.isEmpty {
            return WakeDrawing(kind: .placeholder, title: "", subtitle: subtitle, body: words.newMessage(),
                               threadId: convId, badge: badge, payload: nil)
        }
        return WakeDrawing(kind: .message, title: name, subtitle: subtitle, body: words.requestBody(),
                           threadId: convId, badge: badge, payload: nil)
    }
    let messages = (w["messages"] as? [[String: Any]]) ?? []
    guard let newest = messages.last, let m = newest["message"] as? [String: Any] else {
        return WakeDrawing(kind: .placeholder, title: "", subtitle: subtitle, body: words.newMessage(),
                           threadId: convId, badge: badge, payload: payload())
    }
    let sender = newest["author_name"] as? String ?? ""
    let name = conv["name"] as? String
    let chat = conv["title"] as? String
    let title: String
    if let name, !name.isEmpty {
        title = words.inChannel(sender, name)
    } else if let chat, !chat.isEmpty, chat != sender {
        title = words.inChat(sender, chat)
    } else {
        title = sender
    }
    var body: String
    if !settings.showText {
        body = words.newMessage()
    } else if m["sealed"] is [String: Any] {
        body = open?(m).map(plainText) ?? words.newMessage()
        if body.isEmpty { body = words.newMessage() }
    } else {
        let text = plainText(m["text"] as? String ?? "")
        if !text.isEmpty {
            body = text
        } else if let files = m["files"] as? [Any], !files.isEmpty {
            body = words.sentFile()
        } else if m["form"] is [String: Any] {
            body = words.postedForm()
        } else if let cards = m["cards"] as? [Any], !cards.isEmpty {
            let f = plainText(m["fallback"] as? String ?? "")
            body = f.isEmpty ? words.sentCard() : f
        } else {
            body = words.newMessage()
        }
    }
    let more = messages.count - 1 + ((w["more"] as? NSNumber)?.intValue ?? 0)
    if more > 0 { body = words.withMore(body, more) }
    return WakeDrawing(kind: .message, title: title, subtitle: subtitle, body: body, threadId: convId,
                       badge: badge,
                       payload: payload(message: m["id"] as? String, parent: m["parent_id"] as? String))
}

/// Churr Markdown as one line of plain text, up to 300 characters. A
/// lighter cut than the app's parser: the marks are dropped, spoilers are
/// hidden, and runs of space become one.
public func plainText(_ s: String) -> String {
    var t = s
    // Spoilers first, then the paired marks.
    t = t.replacingOccurrences(of: #"\|\|.+?\|\|"#, with: "•••", options: .regularExpression)
    t = t.replacingOccurrences(of: #"```[a-z0-9+#-]*\n?"#, with: "", options: .regularExpression)
    t = t.replacingOccurrences(of: #"\[([^\]]*)\]\([^)]*\)"#, with: "$1", options: .regularExpression)
    for mark in ["**", "__", "~~", "`", "*", "_"] {
        t = t.replacingOccurrences(of: mark, with: "")
    }
    t = t.replacingOccurrences(of: #"(?m)^\s*(#{1,3}|>|[-+]|\d+\.)\s+(\[[ xX]\]\s+)?"#, with: "",
                               options: .regularExpression)
    t = t.replacingOccurrences(of: #"\s+"#, with: " ", options: .regularExpression)
        .trimmingCharacters(in: .whitespaces)
    if t.count > 300 { t = String(t.prefix(299)) + "…" }
    return t
}
