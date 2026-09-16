import 'package:flutter/foundation.dart';

/// A response from an old filter/project must never replace the current view.
class ResourceController<T> extends ChangeNotifier {
  T? data;
  Object? error;
  bool loading = false;
  bool _disposed = false;
  int _generation = 0;

  Future<void> load(Future<T> Function() fetch) async {
    final generation = ++_generation;
    loading = true;
    data = null;
    error = null;
    notifyListeners();
    try {
      final result = await fetch();
      if (_disposed || generation != _generation) return;
      data = result;
    } catch (e) {
      if (_disposed || generation != _generation) return;
      error = e;
    }
    if (_disposed || generation != _generation) return;
    loading = false;
    notifyListeners();
  }

  @override
  void dispose() {
    _disposed = true;
    _generation++;
    super.dispose();
  }
}
