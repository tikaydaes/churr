// Reading what the app shares with the extension through the keychain
// group both belong to (app/docs/NOTIFY-SHARED.md): the server list, each
// server's tokens and the sealed chats' epoch keys. The app writes them
// with flutter_secure_storage, whose items are generic passwords with the
// service below and the item name as the account.

import Foundation
import Security

public enum SharedItems {
    /// flutter_secure_storage's service name for every item.
    static let service = "flutter_secure_storage_service"
    public static let serversItem = "churr.notify.servers"

    /// The raw bytes of one item, or nil. With no access group given, the
    /// query searches every group of the extension's entitlements.
    public static func read(_ name: String) -> Data? {
        let q: [CFString: Any] = [
            kSecClass: kSecClassGenericPassword,
            kSecAttrService: service,
            kSecAttrAccount: name,
            kSecReturnData: true,
            kSecMatchLimit: kSecMatchLimitOne,
        ]
        var out: CFTypeRef?
        guard SecItemCopyMatching(q as CFDictionary, &out) == errSecSuccess else { return nil }
        return out as? Data
    }

    public static func servers() -> SharedServers? {
        read(serversItem).flatMap(SharedServers.init(json:))
    }

    /// A server's access token, if it has not run out. The extension does
    /// not renew it (see app/README.md, "Notifications").
    public static func accessToken(for server: SharedServer, now: Date = Date()) -> String? {
        guard let d = read(server.tokenKey),
              let j = try? JSONSerialization.jsonObject(with: d) as? [String: Any],
              let access = j["access"] as? String else { return nil }
        if let expires = (j["expires"] as? NSNumber)?.doubleValue,
           Date(timeIntervalSince1970: expires / 1000) <= now { return nil }
        return access
    }

    public static func epoch(server: String, conversation: String, keyEpoch: UInt64) -> SharedEpoch? {
        read("churr.notify.epoch.\(server).\(conversation.lowercased()).\(keyEpoch)")
            .flatMap(SharedEpoch.init(json:))
    }

    /// Opens a sealed message of a wake-up with the shared keys: its text,
    /// or nil (no key for that epoch, a refused message, not a `message`).
    public static func openSealed(_ m: [String: Any], server: String,
                                  epochFor: (String, String, UInt64) -> SharedEpoch? = epoch)
        -> String?
    {
        guard let sealed = m["sealed"] as? [String: Any],
              let keyEpoch = (sealed["key_epoch"] as? NSNumber)?.uint64Value,
              let ct = (sealed["ciphertext"] as? String).flatMap(Data.init(base64URL:)),
              let convS = m["conversation_id"] as? String, let conv = UUID(uuidString: convS),
              let author = (m["author_id"] as? String).flatMap(UUID.init(uuidString:)),
              let seq = (m["seq"] as? NSNumber)?.int64Value,
              let e = epochFor(server, convS, keyEpoch)
        else { return nil }
        let row = SealedRow(conversationId: conv, seq: seq, authorId: author, keyEpoch: keyEpoch,
                            quiet: m["quiet"] as? Bool ?? false, ciphertext: ct)
        return (try? Sealed.open(row, epoch: e))?.text
    }
}

/// The wake-up fetch (`fetchWakeUp`, GET /api/v1/wake-ups/{event_id}),
/// with a ten-second timeout (docs/PUSH.md section 8, step 3).
public enum WakeFetch {
    public static func request(server: SharedServer, token: String, eventId: String) -> URLRequest? {
        guard let escaped = eventId.addingPercentEncoding(withAllowedCharacters: .urlPathAllowed),
              let url = URL(string: "/api/v1/wake-ups/\(escaped)", relativeTo: server.address)
        else { return nil }
        var r = URLRequest(url: url, timeoutInterval: 10)
        r.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
        r.setValue("application/json", forHTTPHeaderField: "Accept")
        return r
    }
}
