import '../../../core/state/resource_controller.dart';
import '../data/reports_repository.dart';
import '../domain/models.dart';

class RunsViewModel extends ResourceController<PageResult<Run>> {
  RunsViewModel(this.repository, this.project, {this.identity});
  final ReportsRepository repository;
  final String project;
  final String? identity;
  String status = '', search = '';
  final List<String?> _cursors = [null];
  bool get hasPrevious => _cursors.length > 1;
  Future<void> refresh() => load(() => identity == null
      ? repository.runs(project, status: status, search: search, cursor: _cursors.last)
      : repository.history(project, identity!, cursor: _cursors.last));
  void filter({String? status, String? search}) {
    this.status = status ?? this.status;
    this.search = search ?? this.search;
    _cursors..clear()..add(null);
    refresh();
  }
  void next() {
    final cursor = data?.nextCursor;
    if (cursor == null) return;
    _cursors.add(cursor);
    refresh();
  }
  void previous() {
    if (!hasPrevious) return;
    _cursors.removeLast();
    refresh();
  }
}

class RunDetailData {
  const RunDetailData(this.run, this.results);
  final Run run;
  final PageResult<TestResult> results;
}

class RunDetailViewModel extends ResourceController<RunDetailData> {
  RunDetailViewModel(this.repository, this.project, this.id);
  final ReportsRepository repository;
  final String project, id;
  final List<String?> _cursors = [null];
  bool get hasPrevious => _cursors.length > 1;
  Future<void> refresh() => load(() async {
    final run = await repository.run(project, id);
    final results = await repository.results(project, id, cursor: _cursors.last);
    return RunDetailData(run, results);
  });
  void next() {
    final cursor = data?.results.nextCursor;
    if (cursor == null) return;
    _cursors.add(cursor);
    refresh();
  }
  void previous() {
    if (!hasPrevious) return;
    _cursors.removeLast();
    refresh();
  }
}

class IdentitiesViewModel extends ResourceController<PageResult<Identity>> {
  IdentitiesViewModel(this.repository, this.project, this.flaky);
  final ReportsRepository repository;
  final String project;
  final bool flaky;
  final List<String?> _cursors = [null];
  bool get hasPrevious => _cursors.length > 1;
  Future<void> refresh() => load(() => repository.identities(project, flaky: flaky, cursor: _cursors.last));
  void next() {
    final cursor = data?.nextCursor;
    if (cursor == null) return;
    _cursors.add(cursor);
    refresh();
  }
  void previous() {
    if (!hasPrevious) return;
    _cursors.removeLast();
    refresh();
  }
}

class ResultViewModel extends ResourceController<ResultDetail> {
  ResultViewModel(this.repository, this.project, this.id);
  final ReportsRepository repository;
  final String project, id;
  bool _seen = false;
  bool busy = false;
  bool _closed = false;
  Object? actionError;
  Future<void> refresh() async {
    await load(() => repository.result(project, id));
    final result = data?.result;
    if (!_closed && !_seen && result != null &&
        const {'failed', 'timed_out', 'interrupted', 'flaky_pass'}.contains(result.status)) {
      await act('FAILURE_SEEN');
    }
  }
  Future<void> act(String type) async {
    if (busy || _closed) return;
    busy = true;
    actionError = null;
    notifyListeners();
    try {
      await repository.triage(project, id, type);
      if (type == 'FAILURE_SEEN') _seen = true;
      if (!_closed) await load(() => repository.result(project, id));
    } catch (e) {
      actionError = e;
    }
    busy = false;
    if (!_closed) notifyListeners();
  }
  @override
  void dispose() {
    _closed = true;
    super.dispose();
  }
}
