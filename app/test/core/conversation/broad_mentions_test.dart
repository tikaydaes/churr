import 'package:churr/core/conversation/broad_mentions.dart';
import 'package:churr/core/models/conversation.dart';
import 'package:flutter_test/flutter_test.dart';

/// Broad mentions in the composer (ruling R64): which words are one, when
/// the box warns, and when the person is asked first.
Conversation _c({bool? allowed, String kind = 'channel', String? rule}) =>
    Conversation.fromJson({
      'id': 'c',
      'kind': kind,
      'name': 'town',
      'broad_mentions': ?rule,
      'broad_mentions_allowed': ?allowed,
      'membership': {'level': 'member', 'read_seq': 0, 'mode': 'mentions'},
    });

const _many = BroadCheck(
  broad: true,
  allowed: true,
  recipients: 4200,
  confirm: true,
);
const _few = BroadCheck(
  broad: true,
  allowed: true,
  recipients: 12,
  confirm: false,
);

void main() {
  test('which words are broad mentions', () {
    const cases = <String, List<String>>{
      '@channel lunch': ['channel'],
      'hello @here and @everyone, and @here again': ['here', 'everyone'],
      '- [ ] tell @channel\n> and @here': ['channel', 'here'],
      '**@channel**': ['channel'],
      'type `@channel` to shout': [],
      '```\n@here\n```': [],
      'mail me@here.example': [],
      '@channels and @hereafter': [],
      '@admins and @sam': [],
      '@here@other.example': [],
      '': [],
    };
    cases.forEach(
      (text, want) => expect(broadMentions(text), want, reason: text),
    );
  });

  test('the box warns only where the conversation says no', () {
    expect(broadGoesPlain('@channel hi', _c(allowed: false)), isTrue);
    expect(broadGoesPlain('hi @sam', _c(allowed: false)), isFalse);
    expect(broadGoesPlain('`@channel`', _c(allowed: false)), isFalse);
    expect(broadGoesPlain('@channel hi', _c(allowed: true)), isFalse);
    expect(broadGoesPlain('@channel hi', _c()), isFalse);
    expect(broadGoesPlain('@channel hi', null), isFalse);
    expect(
      broadGoesPlain('@channel hi', _c(allowed: false, kind: 'sealed')),
      isFalse,
    );
  });

  group('before sending', () {
    // The answer, how often the server was asked, and what was put to the
    // person: "send asked [numbers]".
    Future<String> run(
      String text,
      Conversation? c, {
      BroadCheck? answer,
      bool? says,
    }) async {
      var asked = 0;
      final confirmed = <int>[];
      final send = await broadBeforeSend(
        text,
        c,
        ask: (t) async {
          asked++;
          expect(t, text);
          if (answer == null) throw Exception('offline');
          return answer;
        },
        confirm: (n) async {
          confirmed.add(n);
          return says;
        },
      );
      return '$send $asked $confirmed';
    }

    test('a text with no broad mention is sent without asking', () async {
      expect(await run('hello', _c(allowed: true), answer: _many), 'true 0 []');
    });

    test('where they are not allowed the text goes as it is', () async {
      expect(
        await run('@channel hi', _c(allowed: false), answer: _many),
        'true 0 []',
      );
    });

    test('a few recipients: the server is asked, the person is not', () async {
      expect(
        await run('@here hi', _c(allowed: true), answer: _few),
        'true 1 []',
      );
    });

    test('many recipients: the person is asked with the number', () async {
      expect(
        await run('@channel hi', _c(allowed: true), answer: _many, says: true),
        'true 1 [4200]',
      );
      expect(
        await run('@channel hi', _c(allowed: true), answer: _many, says: false),
        'false 1 [4200]',
      );
      // A dialog dismissed without an answer does not send.
      expect(
        await run('@channel hi', _c(allowed: true), answer: _many),
        'false 1 [4200]',
      );
    });

    test('a conversation that does not say: the server is asked', () async {
      expect(
        await run('@everyone hi', _c(), answer: _many, says: true),
        'true 1 [4200]',
      );
    });

    test('the server says no after all: sent, nothing to confirm', () async {
      const no = BroadCheck(
        broad: true,
        allowed: false,
        recipients: 0,
        confirm: false,
      );
      expect(
        await run('@channel hi', _c(allowed: true), answer: no),
        'true 1 []',
      );
    });

    test('offline: sent, and the post decides', () async {
      expect(await run('@channel hi', _c(allowed: true)), 'true 1 []');
    });

    test('a sealed chat asks nothing', () async {
      expect(
        await run('@channel hi', _c(kind: 'sealed'), answer: _many),
        'true 0 []',
      );
    });
  });

  test('the answer of checkBroadMentions is read', () {
    final b = BroadCheck.fromJson({
      'broad': true,
      'allowed': true,
      'recipients': 31,
      'confirm': true,
    });
    expect(
      (b.broad, b.allowed, b.recipients, b.confirm),
      (true, true, 31, true),
    );
    final none = BroadCheck.fromJson(const {});
    expect(
      (none.broad, none.allowed, none.recipients, none.confirm),
      (false, false, 0, false),
    );
  });

  test('the membership carries "ignore broad mentions" there and back', () {
    final m = Membership.fromJson({
      'level': 'member',
      'read_seq': 3,
      'mode': 'mentions',
      'ignore_broad_mentions': true,
    });
    expect(m.ignoreBroadMentions, isTrue);
    expect(m.toJson()['ignore_broad_mentions'], isTrue);
    expect(m.copyWith(readSeq: 4).ignoreBroadMentions, isTrue);
    expect(
      Membership.fromJson({'level': 'member'}).ignoreBroadMentions,
      isFalse,
    );
  });

  test('the conversation says who may, and whether its reader may', () {
    expect(_c().broadMentions, 'server');
    expect(_c().broadMentionsAllowed, isNull);
    expect(_c(rule: 'managers', allowed: false).broadMentions, 'managers');
    expect(_c(rule: 'managers', allowed: false).broadMentionsAllowed, isFalse);
  });

  group('a conversation from an event sent to everyone', () {
    final held = _c(rule: 'server', allowed: false);
    final event = {'id': 'c', 'kind': 'channel', 'name': 'town hall'};

    test('keeps the membership and the answer held', () {
      final out = Conversation.fromJson(
        keepOwn({...event, 'broad_mentions': 'server'}, held),
      );
      expect(out.name, 'town hall');
      expect(out.membership?.level, 'member');
      expect(out.broadMentionsAllowed, isFalse);
    });

    test('drops the answer when the channel\'s rule changed', () {
      final out = Conversation.fromJson(
        keepOwn({...event, 'broad_mentions': 'anyone'}, held),
      );
      expect(out.broadMentions, 'anyone');
      expect(out.broadMentionsAllowed, isNull);
      expect(out.membership, isNotNull);
    });

    test('takes what the event does carry, and nothing from nowhere', () {
      final own = keepOwn({
        ...event,
        'broad_mentions': 'server',
        'broad_mentions_allowed': true,
        'membership': {'level': 'manager'},
      }, held);
      expect(own['broad_mentions_allowed'], isTrue);
      expect((own['membership'] as Map)['level'], 'manager');
      expect(keepOwn(event, null), event);
    });
  });
}
