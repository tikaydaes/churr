// Checking and decrypting one sealed message for a notification
// (docs/SEALED.md sections 8.1, 8.2 and 11). CryptoKit only: HKDF-SHA-256,
// one Ed25519 verification and one AES-256-GCM decryption. It never makes
// an epoch, never signs and changes no stored key. The counter checks of
// section 8.2 need the whole history and are left to the app.
//
// Checked against testdata/sealed/message.json by
// Tests/ChurrNotifyTests/VectorTests.swift (`swift test` in this folder).

import CryptoKit
import Foundation

/// What the app shares for one key epoch of one sealed chat
/// (app/docs/NOTIFY-SHARED.md, item `churr.notify.epoch.…`).
public struct SharedEpoch: Equatable {
    public struct Sender: Equatable {
        public let accountId: UUID
        public let deviceId: UUID
        public let signKey: Data
        public init(accountId: UUID, deviceId: UUID, signKey: Data) {
            self.accountId = accountId
            self.deviceId = deviceId
            self.signKey = signKey
        }
    }

    public let conversationId: UUID
    public let keyEpoch: UInt64
    /// The sequence number of the epoch record in the chat.
    public let epochSeq: Int64
    /// SEALED.md 5.2's content key, 32 bytes.
    public let contentKey: Data
    public let senders: [Sender]

    public init(conversationId: UUID, keyEpoch: UInt64, epochSeq: Int64, contentKey: Data, senders: [Sender]) {
        self.conversationId = conversationId
        self.keyEpoch = keyEpoch
        self.epochSeq = epochSeq
        self.contentKey = contentKey
        self.senders = senders
    }

    /// Reads the item's JSON.
    public init?(json: Data) {
        guard let j = try? JSONSerialization.jsonObject(with: json) as? [String: Any],
              (j["v"] as? Int) == 1,
              let conv = (j["conversation_id"] as? String).flatMap(UUID.init(uuidString:)),
              let epoch = (j["key_epoch"] as? NSNumber)?.uint64Value,
              let seq = (j["epoch_seq"] as? NSNumber)?.int64Value,
              let key = (j["content_key"] as? String).flatMap(Data.init(base64URL:)),
              key.count == 32,
              let list = j["senders"] as? [[String: Any]]
        else { return nil }
        var senders: [Sender] = []
        for s in list {
            guard let a = (s["account_id"] as? String).flatMap(UUID.init(uuidString:)),
                  let d = (s["device_id"] as? String).flatMap(UUID.init(uuidString:)),
                  let k = (s["sign_key"] as? String).flatMap(Data.init(base64URL:)), k.count == 32
            else { return nil }
            senders.append(Sender(accountId: a, deviceId: d, signKey: k))
        }
        self.init(conversationId: conv, keyEpoch: epoch, epochSeq: seq, contentKey: key, senders: senders)
    }
}

/// The row a sealed message arrived in, as the server sent it.
public struct SealedRow {
    public let conversationId: UUID
    public let seq: Int64
    public let authorId: UUID
    public let keyEpoch: UInt64
    public let quiet: Bool
    public let ciphertext: Data
    public init(conversationId: UUID, seq: Int64, authorId: UUID, keyEpoch: UInt64, quiet: Bool, ciphertext: Data) {
        self.conversationId = conversationId
        self.seq = seq
        self.authorId = authorId
        self.keyEpoch = keyEpoch
        self.quiet = quiet
        self.ciphertext = ciphertext
    }
}

/// Why a message was refused. A refused message is never drawn.
public enum SealedRefusal: Error, Equatable {
    case malformed
    case format
    case rowMismatch
    case beforeEpoch
    case notARecipient
    case badSignature
    case doesNotDecrypt
    case quietMismatch
}

/// An opened message: the header's fields and the content bytes.
public struct OpenedMessage {
    public let deviceId: UUID
    public let counter: UInt64
    public let reference: UUID
    public let quiet: Bool
    /// The UTF-8 JSON of SEALED.md section 8.3.
    public let content: Data

    /// The text to show, for a `message`; nil for every other kind
    /// (edits, reactions and the rest are quiet and never shown).
    public var text: String? {
        guard let j = try? JSONSerialization.jsonObject(with: content) as? [String: Any],
              (j["kind"] as? String) == "message" else { return nil }
        return j["text"] as? String ?? ""
    }
}

public enum Sealed {
    static let headerSize = 82
    static let saltSize = 16
    static let tagSize = 16
    static let signatureSize = 64
    static let labelLockMessage = "churr lock message v1"
    static let labelMessage = "churr sealed message v1"
    static let labelSender = "churr sender v1"
    static let labelContent = "churr content v1"

    static func labelled(_ label: String, _ rest: Data) -> Data {
        var d = Data(label.utf8)
        d.append(0)
        d.append(rest)
        return d
    }

    static func hkdf(_ ikm: Data, salt: Data, info: Data, count: Int) -> Data {
        let k = HKDF<SHA256>.deriveKey(
            inputKeyMaterial: SymmetricKey(data: ikm), salt: salt, info: info, outputByteCount: count)
        return k.withUnsafeBytes { Data($0) }
    }

    static func u64(_ v: UInt64) -> Data {
        var be = v.bigEndian
        return Data(bytes: &be, count: 8)
    }

    static func readU64(_ d: Data, _ at: Int) -> UInt64 {
        d[d.startIndex + at ..< d.startIndex + at + 8].reduce(0) { ($0 << 8) | UInt64($1) }
    }

    /// The salt of every epoch derivation: conversation id || key epoch.
    static func epochSalt(_ conv: UUID, _ epoch: UInt64) -> Data {
        conv.data + u64(epoch)
    }

    /// SEALED.md 5.2: content key from a history secret (only the tests
    /// need it; the app shares the content key itself).
    public static func contentKey(historySecret: Data, conversation: UUID, epoch: UInt64) -> Data {
        hkdf(historySecret, salt: epochSalt(conversation, epoch), info: Data(labelContent.utf8), count: 32)
    }

    /// SEALED.md 5.2: a device's sender key under an epoch's content key.
    public static func senderKey(contentKey: Data, conversation: UUID, epoch: UInt64, device: UUID) -> Data {
        hkdf(contentKey, salt: epochSalt(conversation, epoch),
             info: labelled(labelSender, device.data), count: 32)
    }

    /// Checks and decrypts [row] with what [epoch] holds (SEALED.md 8.2,
    /// "Receiving", steps 1 to 3, and the quiet flag of 8.4).
    public static func open(_ row: SealedRow, epoch: SharedEpoch) throws -> OpenedMessage {
        let b = row.ciphertext
        guard b.count >= headerSize + saltSize + tagSize + signatureSize else { throw SealedRefusal.malformed }
        let s = b.startIndex
        let format = b[s], flags = b[s + 1]
        guard format == 1, flags & ~1 == 0 else { throw SealedRefusal.format }
        let conv = UUID(data: b[s + 2 ..< s + 18])
        let keyEpoch = readU64(b, 18)
        let account = UUID(data: b[s + 26 ..< s + 42])
        let device = UUID(data: b[s + 42 ..< s + 58])
        let counter = readU64(b, 58)
        let reference = UUID(data: b[s + 66 ..< s + 82])
        // 1. The header agrees with the row it arrived in, and the epoch
        //    shared is that one.
        guard conv == row.conversationId, keyEpoch == row.keyEpoch, account == row.authorId,
              conv == epoch.conversationId, keyEpoch == epoch.keyEpoch
        else { throw SealedRefusal.rowMismatch }
        guard row.seq > epoch.epochSeq else { throw SealedRefusal.beforeEpoch }
        // 2. The sending device is a recipient of the epoch, on that account.
        guard let sender = epoch.senders.first(where: { $0.deviceId == device && $0.accountId == account })
        else { throw SealedRefusal.notARecipient }
        // 3. The signature over everything before it, then the content.
        let n = b.count - signatureSize
        let body = b[s ..< s + n]
        let sig = b[s + n ..< b.endIndex]
        guard let pub = try? Curve25519.Signing.PublicKey(rawRepresentation: sender.signKey),
              pub.isValidSignature(sig, for: labelled(labelMessage, Data(body)))
        else { throw SealedRefusal.badSignature }
        let header = Data(b[s ..< s + headerSize])
        let salt = Data(b[s + headerSize ..< s + headerSize + saltSize])
        let sealed = Data(b[s + headerSize + saltSize ..< s + n])
        let sk = senderKey(contentKey: epoch.contentKey, conversation: conv, epoch: keyEpoch, device: device)
        let okm = hkdf(sk, salt: salt, info: labelled(labelLockMessage, header), count: 44)
        let content: Data
        do {
            let box = try AES.GCM.SealedBox(
                nonce: AES.GCM.Nonce(data: okm.suffix(12)),
                ciphertext: sealed.prefix(sealed.count - tagSize),
                tag: sealed.suffix(tagSize))
            content = try AES.GCM.open(
                box, using: SymmetricKey(data: okm.prefix(32)),
                authenticating: labelled(labelLockMessage, header))
        } catch {
            throw SealedRefusal.doesNotDecrypt
        }
        // The quiet flag is under the signature: the row must agree.
        let quiet = flags & 1 == 1
        guard quiet == row.quiet else { throw SealedRefusal.quietMismatch }
        return OpenedMessage(deviceId: device, counter: counter, reference: reference, quiet: quiet, content: content)
    }
}

extension UUID {
    /// The identifier's 16 bytes.
    var data: Data {
        withUnsafeBytes(of: uuid) { Data($0) }
    }

    init(data: Data) {
        var t: uuid_t = (0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0)
        withUnsafeMutableBytes(of: &t) { $0.copyBytes(from: data.prefix(16)) }
        self.init(uuid: t)
    }
}

extension Data {
    /// base64url without padding, as the API and the shared items carry
    /// bytes.
    public init?(base64URL s: String) {
        var b = s.replacingOccurrences(of: "-", with: "+").replacingOccurrences(of: "_", with: "/")
        while b.count % 4 != 0 { b.append("=") }
        self.init(base64Encoded: b)
    }

    /// Lower-case hexadecimal, as the APNs token is registered.
    public var hex: String { map { String(format: "%02x", $0) }.joined() }
}
