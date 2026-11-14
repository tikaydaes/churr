// The sealed-message vectors of testdata/sealed (docs/SEALED.md section
// 18) against the extension's Swift decrypt: every message of
// message.json opens to its content, and every row of its `receive` run is
// taken or refused as the extension must (counter checks excepted: those
// are the app's, SEALED.md section 11). Run with `swift test` from
// app/ios/NotificationService.

import CryptoKit
import Foundation
import XCTest

@testable import ChurrNotify

final class VectorTests: XCTestCase {
    /// testdata/sealed, found from this file's place in the repository.
    static let dir: URL = {
        var u = URL(fileURLWithPath: #filePath)
        for _ in 0 ..< 6 { u.deleteLastPathComponent() }  // …/app/ios/NotificationService/Tests/ChurrNotifyTests/x
        return u.appendingPathComponent("testdata/sealed")
    }()

    func load(_ name: String) throws -> [String: Any] {
        let d = try Data(contentsOf: Self.dir.appendingPathComponent(name))
        return try XCTUnwrap(JSONSerialization.jsonObject(with: d) as? [String: Any])
    }

    func hex(_ s: Any?) -> Data {
        let str = s as? String ?? ""
        var d = Data(capacity: str.count / 2)
        var i = str.startIndex
        while i < str.endIndex {
            let j = str.index(i, offsetBy: 2)
            d.append(UInt8(str[i ..< j], radix: 16)!)
            i = j
        }
        return d
    }

    func uuid(_ s: Any?) -> UUID { UUID(uuidString: s as! String)! }

    /// The epoch item the app would share for epoch 1 of message.json:
    /// its content key, the record's sequence number, and each recipient
    /// device of epoch_record.json's epoch_1 with its signing key.
    func sharedEpoch() throws -> (SharedEpoch, [String: Any]) {
        let v = try load("message.json")
        let er = try load("epoch_record.json")
        let devices = (er["devices"] as! [[String: Any]])
        let signKeys = Dictionary(uniqueKeysWithValues: devices.map { (uuid($0["device_id"]), hex($0["sign_key"])) })
        let fields = (er["epoch_1"] as! [String: Any])["fields"] as! [String: Any]
        let senders = (fields["recipients"] as! [[String: Any]]).map {
            SharedEpoch.Sender(accountId: uuid($0["account_id"]), deviceId: uuid($0["device_id"]),
                               signKey: signKeys[uuid($0["device_id"])]!)
        }
        let e = SharedEpoch(
            conversationId: uuid(v["conversation_id"]),
            keyEpoch: UInt64(v["key_epoch"] as! Int),
            epochSeq: Int64(v["epoch_seq"] as! Int),
            contentKey: hex(v["content_key"]),
            senders: senders)
        return (e, v)
    }

    func testContentAndSenderKeys() throws {
        let (e, v) = try sharedEpoch()
        XCTAssertEqual(
            Sealed.contentKey(historySecret: hex(v["history_secret"]), conversation: e.conversationId, epoch: e.keyEpoch),
            e.contentKey, "content key from the history secret")
        for m in v["messages"] as! [[String: Any]] {
            let h = m["header"] as! [String: Any]
            XCTAssertEqual(
                Sealed.senderKey(contentKey: e.contentKey, conversation: e.conversationId, epoch: e.keyEpoch,
                                 device: uuid(h["device_id"])),
                hex(m["sender_key"]), "sender key of \(m["kind"]!)")
        }
    }

    func testEveryMessageOpens() throws {
        let (e, v) = try sharedEpoch()
        for m in v["messages"] as! [[String: Any]] {
            let h = m["header"] as! [String: Any]
            let row = SealedRow(
                conversationId: e.conversationId, seq: 100, authorId: uuid(h["account_id"]),
                keyEpoch: e.keyEpoch, quiet: m["quiet"] as! Bool, ciphertext: hex(m["sealed"]))
            let opened = try Sealed.open(row, epoch: e)
            XCTAssertEqual(opened.content, hex(m["content_bytes"]), "content of \(m["kind"]!)")
            XCTAssertEqual(opened.counter, UInt64(h["counter"] as! Int))
            XCTAssertEqual(opened.reference, uuid(h["reference"]))
            XCTAssertEqual(opened.quiet, m["quiet"] as! Bool)
            // Only an ordinary message has text to show.
            if (m["kind"] as! String) == "message" {
                XCTAssertNotNil(opened.text)
            } else {
                XCTAssertNil(opened.text)
            }
        }
    }

    func testTheReceiveRun() throws {
        let (e, v) = try sharedEpoch()
        var refused = 0, taken = 0
        for r in v["receive"] as! [[String: Any]] {
            let row = r["row"] as! [String: Any]
            let sealed = SealedRow(
                conversationId: e.conversationId, seq: Int64(row["seq"] as! Int),
                authorId: uuid(row["author_id"]), keyEpoch: UInt64(row["key_epoch"] as! Int),
                quiet: row["quiet"] as! Bool, ciphertext: hex(row["ciphertext"]))
            let outcome = r["outcome"] as! String
            let why = r["why"] as! String
            if outcome == "refused" {
                XCTAssertThrowsError(try Sealed.open(sealed, epoch: e), "must refuse: \(why)")
                refused += 1
            } else {
                // shown, shown_with_gap, shown_out_of_order, dropped: the
                // message itself is sound; what differs is the counters,
                // which only the app can judge.
                XCTAssertNoThrow(try Sealed.open(sealed, epoch: e), "must take: \(why)")
                taken += 1
            }
        }
        XCTAssertGreaterThan(refused, 0)
        XCTAssertGreaterThan(taken, 0)
    }

    func testRefusalsByKind() throws {
        let (e, v) = try sharedEpoch()
        let m = (v["messages"] as! [[String: Any]])[0]
        let h = m["header"] as! [String: Any]
        let good = SealedRow(conversationId: e.conversationId, seq: 50, authorId: uuid(h["account_id"]),
                             keyEpoch: e.keyEpoch, quiet: false, ciphertext: hex(m["sealed"]))
        func refusal(_ r: SealedRow, _ ep: SharedEpoch? = nil) -> SealedRefusal? {
            do { _ = try Sealed.open(r, epoch: ep ?? e); return nil } catch { return error as? SealedRefusal }
        }
        XCTAssertNil(refusal(good))
        XCTAssertEqual(refusal(SealedRow(conversationId: e.conversationId, seq: 1, authorId: good.authorId,
                                         keyEpoch: e.keyEpoch, quiet: false, ciphertext: good.ciphertext)), .beforeEpoch)
        XCTAssertEqual(refusal(SealedRow(conversationId: e.conversationId, seq: 50, authorId: good.authorId,
                                         keyEpoch: e.keyEpoch, quiet: true, ciphertext: good.ciphertext)), .quietMismatch)
        var flipped = good.ciphertext
        flipped[flipped.startIndex + 100] ^= 1
        XCTAssertEqual(refusal(SealedRow(conversationId: e.conversationId, seq: 50, authorId: good.authorId,
                                         keyEpoch: e.keyEpoch, quiet: false, ciphertext: flipped)), .badSignature)
        let noSenders = SharedEpoch(conversationId: e.conversationId, keyEpoch: e.keyEpoch, epochSeq: e.epochSeq,
                                    contentKey: e.contentKey, senders: [])
        XCTAssertEqual(refusal(good, noSenders), .notARecipient)
        let wrongKey = SharedEpoch(conversationId: e.conversationId, keyEpoch: e.keyEpoch, epochSeq: e.epochSeq,
                                   contentKey: Data(repeating: 7, count: 32), senders: e.senders)
        XCTAssertEqual(refusal(good, wrongKey), .doesNotDecrypt)
    }

    func testTheSharedItemFormat() throws {
        let (e, _) = try sharedEpoch()
        func b64(_ d: Data) -> String {
            d.base64EncodedString().replacingOccurrences(of: "+", with: "-")
                .replacingOccurrences(of: "/", with: "_").replacingOccurrences(of: "=", with: "")
        }
        let item: [String: Any] = [
            "v": 1, "conversation_id": e.conversationId.uuidString.lowercased(),
            "key_epoch": e.keyEpoch, "epoch_seq": e.epochSeq, "content_key": b64(e.contentKey),
            "senders": e.senders.map {
                ["account_id": $0.accountId.uuidString.lowercased(),
                 "device_id": $0.deviceId.uuidString.lowercased(), "sign_key": b64($0.signKey)]
            },
        ]
        let parsed = SharedEpoch(json: try JSONSerialization.data(withJSONObject: item))
        XCTAssertEqual(parsed, e)
    }

    func testAWakeUpForASealedChatIsDrawnFromTheSharedKeys() throws {
        let (e, v) = try sharedEpoch()
        let m = (v["messages"] as! [[String: Any]])[0]
        let h = m["header"] as! [String: Any]
        func b64(_ d: Data) -> String {
            d.base64EncodedString().replacingOccurrences(of: "+", with: "-")
                .replacingOccurrences(of: "/", with: "_").replacingOccurrences(of: "=", with: "")
        }
        let answer: [String: Any] = ["wake_up": [
            "kind": "message",
            "conversation": ["id": e.conversationId.uuidString.lowercased(), "kind": "sealed", "title": "Alice"],
            "messages": [["author_name": "Alice", "message": [
                "id": "row-2", "conversation_id": e.conversationId.uuidString.lowercased(), "seq": 2,
                "author_id": (h["account_id"] as! String), "quiet": false,
                "created_at": "2027-01-15T08:00:00Z",
                "sealed": ["key_epoch": 1, "ciphertext": b64(hex(m["sealed"]))],
            ]]],
            "more": 0, "read": false, "badge": 3,
        ]]
        let server = SharedServer(id: "s1", address: URL(string: "https://chat.example.org")!, name: "Home",
                                  route: "r", tokenKey: "churr.tokens.s1")
        let settings = SharedServers(json: try JSONSerialization.data(withJSONObject: [
            "servers": [["id": "s1", "address": "https://chat.example.org", "name": "Home", "route": "r"]],
            "show_text": true, "multi": false,
        ]))!
        let d = drawWakeUp(try JSONSerialization.data(withJSONObject: answer), server: server, settings: settings,
                           words: EnglishWords(), open: { msg in
                               SharedItems.openSealed(msg, server: "s1", epochFor: { _, _, _ in e })
                           })
        XCTAssertEqual(d?.kind, .message)
        XCTAssertEqual(d?.title, "Alice")
        XCTAssertEqual(d?.body, "Hello Bob")
        XCTAssertEqual(d?.badge, 3)
        XCTAssertEqual(d?.threadId, e.conversationId.uuidString.lowercased())
        // Without the key for that epoch: the placeholder.
        let none = drawWakeUp(try JSONSerialization.data(withJSONObject: answer), server: server, settings: settings,
                              words: EnglishWords(), open: { msg in
                                  SharedItems.openSealed(msg, server: "s1", epochFor: { _, _, _ in nil })
                              })
        XCTAssertEqual(none?.body, "New message")
    }

    func testPlainWakeUpsAndRoutes() throws {
        let settings = SharedServers(json: Data(#"""
            {"v":1,"show_text":true,"multi":true,"servers":[{"id":"s1","address":"https://a.example","name":"Work","route":"q83vEjRWeJA","token_key":"churr.tokens.s1"}]}
            """#.utf8))!
        let server = try XCTUnwrap(settings.server(forEvent: "q83vEjRWeJA.TOKEN"))
        XCTAssertNil(settings.server(forEvent: "other.TOKEN"))
        XCTAssertEqual(WakeFetch.request(server: server, token: "t", eventId: "q83vEjRWeJA.TOKEN")?.url?.absoluteString,
                       "https://a.example/api/v1/wake-ups/q83vEjRWeJA.TOKEN")
        let answer = Data(#"""
            {"wake_up":{"kind":"message","conversation":{"id":"c1","kind":"channel","name":"ops"},
            "messages":[{"author_name":"Sam","message":{"id":"m1","conversation_id":"c1","seq":4,"author_id":"a","text":"**Deploy** now ||secret||","created_at":"2026-11-14T00:00:00Z"}},
                        {"author_name":"Sam","message":{"id":"m2","conversation_id":"c1","seq":5,"author_id":"a","parent_id":"m1","text":"","files":[{"id":"f"}],"created_at":"2026-11-14T00:00:00Z"}}],
            "more":0,"read":false,"badge":7}}
            """#.utf8)
        let d = try XCTUnwrap(drawWakeUp(answer, server: server, settings: settings, words: EnglishWords()))
        XCTAssertEqual(d.title, "Sam in #ops")
        XCTAssertEqual(d.subtitle, "Work")
        XCTAssertEqual(d.body, "Sent a file (+1 more)")
        XCTAssertEqual(d.badge, 7)
        XCTAssertEqual(d.payload, #"{"c":"c1","m":"m2","p":"m1","s":"s1"}"#)
        XCTAssertEqual(plainText("**Deploy** now ||secret||"), "Deploy now •••")
        let ring = Data(#"""
            {"wake_up":{"kind":"ring","conversation":{"id":"c1","kind":"channel","title":"Sam"},"messages":[],"more":0,"read":false,
            "call":{"id":"k1","from_account_id":"a","from_name":"Sam","ringing":true},"badge":1}}
            """#.utf8)
        let r = try XCTUnwrap(drawWakeUp(ring, server: server, settings: settings, words: EnglishWords()))
        XCTAssertEqual(r.kind, .ring)
        XCTAssertEqual(r.title, "Sam is calling")
        XCTAssertTrue(r.payload!.contains(#""call":"k1""#))
    }
}

/// The English wording, as the extension's Localizable.strings has it.
struct EnglishWords: Words {
    func newMessage() -> String { "New message" }
    func sentFile() -> String { "Sent a file" }
    func postedForm() -> String { "Posted a poll or a form" }
    func sentCard() -> String { "Sent a card" }
    func inChannel(_ s: String, _ c: String) -> String { "\(s) in #\(c)" }
    func inChat(_ s: String, _ c: String) -> String { "\(s) in \(c)" }
    func calling(_ n: String) -> String { "\(n) is calling" }
    func callBody() -> String { "Open to join or decline" }
    func missedCall(_ n: String) -> String { "Missed call from \(n)" }
    func withMore(_ b: String, _ n: Int) -> String { "\(b) (+\(n) more)" }
}
