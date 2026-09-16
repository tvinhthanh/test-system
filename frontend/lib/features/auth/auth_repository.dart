import 'package:flutter/foundation.dart';
import '../../core/api/api_client.dart';

class Session {
  const Session(this.email, this.name);
  final String email;
  final String name;
  factory Session.fromJson(Map<String, dynamic> json) =>
      Session(json['email'] as String, json['name'] as String);
}

class AuthRepository extends ChangeNotifier {
  AuthRepository(this.api) {
    api.onUnauthorized = invalidate;
  }
  final ApiClient api;
  Session? session;
  bool ready = false;
  Object? bootstrapError;

  Future<void> initialize() async {
    bootstrapError = null;
    try {
      final result = await api.get('/v1/session');
      api.csrfToken = result['csrfToken'] as String;
      session = result['user'] == null ? null : Session.fromJson(object(result['user']));
    } catch (e) {
      bootstrapError = e;
    }
    ready = true;
    notifyListeners();
  }

  Future<void> login(String email, String password) async {
    if (api.csrfToken == null) await initialize();
    final result = await api.post('/v1/session', {'email': email, 'password': password});
    api.csrfToken = result['csrfToken'] as String;
    session = Session.fromJson(object(result['user']));
    notifyListeners();
  }

  Future<void> logout() async {
    await api.post('/v1/session/logout', {});
    invalidate();
  }

  void invalidate() {
    session = null;
    api.csrfToken = null;
    notifyListeners();
  }
}
