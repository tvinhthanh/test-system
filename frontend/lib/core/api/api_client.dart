import 'dart:async';
import 'dart:convert';
import 'package:http/http.dart' as http;

class ApiException implements Exception {
  const ApiException(this.status, this.code, this.message, {this.requestId});
  final int status;
  final String code;
  final String message;
  final String? requestId;
  @override
  String toString() => message;
}

Map<String, dynamic> object(Object? value) {
  if (value is! Map<String, dynamic>) {
    throw const FormatException('Expected a JSON object');
  }
  return value;
}

class ApiClient {
  ApiClient(this._client, this.baseUrl);
  final http.Client _client;
  final Uri baseUrl;
  String? csrfToken;
  void Function()? onUnauthorized;

  Future<Map<String, dynamic>> get(String path,
          [Map<String, String>? query]) =>
      request('GET', path, query: query);

  Future<Map<String, dynamic>> post(String path, Map<String, Object?> body) =>
      request('POST', path, body: body);

  Future<Map<String, dynamic>> request(String method, String path,
      {Map<String, String>? query, Map<String, Object?>? body}) async {
    final uri = baseUrl.resolve(path).replace(queryParameters: query);
    final headers = <String, String>{'Accept': 'application/json'};
    if (body != null) headers['Content-Type'] = 'application/json';
    if (method != 'GET') {
      final token = csrfToken;
      if (token == null || token.isEmpty) {
        throw const ApiException(0, 'CSRF_REQUIRED', 'Refresh your session and try again.');
      }
      headers['X-CSRF-Token'] = token;
    }
    try {
      final req = http.Request(method, uri)..headers.addAll(headers);
      if (body != null) req.body = jsonEncode(body);
      final response = await (() async => http.Response.fromStream(await _client.send(req)))()
          .timeout(const Duration(seconds: 20));
      if (response.statusCode == 401) onUnauthorized?.call();
      Map<String, dynamic> data = {};
      try {
        if (response.body.isNotEmpty) data = object(jsonDecode(response.body));
      } on FormatException {
        if (response.statusCode >= 200 && response.statusCode < 300) {
          throw const ApiException(0, 'INVALID_RESPONSE', 'The server returned an invalid response.');
        }
      }
      if (response.statusCode < 200 || response.statusCode >= 300) {
        final error = data['error'];
        final envelope = error is Map<String, dynamic> ? error : <String, dynamic>{};
        throw ApiException(response.statusCode, envelope['code'] as String? ?? 'HTTP_ERROR',
            switch (response.statusCode) {
              401 => 'Your session has expired. Sign in again.',
              403 => 'You do not have permission for this action.',
              404 => 'This resource is unavailable.',
              429 => 'Too many requests. Please try again shortly.',
              _ => 'The request could not be completed. Please try again.',
            }, requestId: response.headers['x-request-id']);
      }
      return data;
    } on TimeoutException {
      throw const ApiException(0, 'TIMEOUT', 'The request timed out. Please try again.');
    } on http.ClientException {
      throw const ApiException(0, 'NETWORK', 'Cannot connect to the server.');
    }
  }

  void close() => _client.close();
}
