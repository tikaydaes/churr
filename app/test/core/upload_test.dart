import 'dart:convert';
import 'dart:io';

import 'package:churr/core/api/client.dart';
import 'package:churr/core/api/conversation_ops.dart';
import 'package:churr/core/session_store.dart';
import 'package:flutter_test/flutter_test.dart';

/// Uploads go out as real multipart forms: a local HTTP server checks the
/// Content-Type carries its boundary and parses the body with it. (The
/// headers were once copied before the form was finalised, and every
/// upload went without its Content-Type.)
void main() {
  test('uploadFile sends a multipart form with its boundary', () async {
    final server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
    addTearDown(() => server.close(force: true));
    String? seenType;
    Map<String, String> fields = {};
    List<int>? fileBytes;
    String? fileName;
    server.listen((req) async {
      seenType = req.headers.value('content-type');
      final body = await req.fold<List<int>>([], (a, b) => a..addAll(b));
      final boundary = RegExp(r'boundary=("?)([^";]+)\1')
          .firstMatch(seenType ?? '')
          ?.group(2);
      var status = 400;
      if (boundary != null) {
        final text = latin1.decode(body);
        for (final part in text.split('--$boundary')) {
          final i = part.indexOf('\r\n\r\n');
          if (i < 0) continue;
          final head = part.substring(0, i);
          var content = part.substring(i + 4);
          if (content.endsWith('\r\n')) {
            content = content.substring(0, content.length - 2);
          }
          final name = RegExp(r'name="([^"]*)"').firstMatch(head)?.group(1);
          final file = RegExp(r'filename="([^"]*)"').firstMatch(head)?.group(1);
          if (name == 'file') {
            fileBytes = latin1.encode(content);
            fileName = file;
          } else if (name != null) {
            fields[name] = utf8.decode(latin1.encode(content));
          }
        }
        status = fileBytes == null ? 400 : 201;
      }
      req.response
        ..statusCode = status
        ..headers.contentType = ContentType.json
        ..write(
          jsonEncode(
            status == 201
                ? {
                    'file': {
                      'id': 'f1',
                      'name': fileName,
                      'size': fileBytes!.length,
                      'media_type': 'image/png',
                    },
                  }
                : {
                    'error': {'code': 'invalid_request', 'message': 'no form'},
                  },
          ),
        );
      await req.response.close();
    });
    final api = ApiClient(
      base: Uri.parse('http://127.0.0.1:${server.port}'),
      tokens: Tokens(
        access: 'churr_app_t',
        refresh: '',
        expiresAt: DateTime.now().add(const Duration(hours: 1)),
      ),
    );
    addTearDown(api.close);
    final bytes = List<int>.generate(3000, (i) => i % 256);
    var progressed = 0;
    final f = await api.uploadFile(
      'conv-1',
      name: 'picture.png',
      bytes: bytes,
      caption: 'A caption',
      onProgress: (sent, total) => progressed = sent,
    );
    expect(seenType, startsWith('multipart/form-data; boundary='));
    expect(fileName, 'picture.png');
    expect(fileBytes, bytes);
    expect(fields['caption'], 'A caption');
    expect(f.id, 'f1');
    expect(progressed, greaterThan(3000));
  });
}
