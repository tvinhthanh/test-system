import '../../../core/api/api_client.dart';

class Project {
  const Project(this.id, this.name, this.workspace);
  final String id;
  final String name;
  final String workspace;
  factory Project.fromJson(Map<String, dynamic> j) => Project(j['id'] as String, j['name'] as String, j['workspaceName'] as String);
}

class PageResult<T> {
  const PageResult(this.items, this.nextCursor);
  final List<T> items;
  final String? nextCursor;
  factory PageResult.fromJson(Map<String, dynamic> j, T Function(Map<String, dynamic>) parse) =>
      PageResult((j['items'] as List).map((e) => parse(object(e))).toList(), j['nextCursor'] as String?);
}

class Run {
  const Run({required this.id, required this.branch, required this.commit, required this.status,
    required this.environment, required this.startedAt, required this.durationMs,
    required this.total, required this.passed, required this.failed, required this.flakyCount});
  final String id, branch, commit, status, environment;
  final DateTime startedAt;
  final int? durationMs;
  final int total, passed, failed, flakyCount;
  bool get warning => status == 'passed' && flakyCount > 0;
  factory Run.fromJson(Map<String, dynamic> j) => Run(id: j['id'] as String,
      branch: j['branch'] as String, commit: j['commitSha'] as String, status: j['status'] as String,
      environment: j['environmentName'] as String, startedAt: DateTime.parse(j['startedAt'] as String),
      durationMs: j['durationMs'] as int?, total: j['totalCount'] as int, passed: j['passedCount'] as int,
      failed: j['failedCount'] as int, flakyCount: j['flakyCount'] as int);
}

class TestResult {
  const TestResult(this.id, this.identityId, this.name, this.file, this.status, this.attempts, this.durationMs);
  final String id, name, file, status;
  final String? identityId;
  final int attempts, durationMs;
  factory TestResult.fromJson(Map<String, dynamic> j) => TestResult(j['id'] as String,
      j['stableTestId'] as String?, j['displayName'] as String, j['filePath'] as String,
      j['finalStatus'] as String, j['attemptCount'] as int, j['durationMs'] as int);
}

class Attempt {
  const Attempt(this.index, this.status, this.durationMs, this.message, this.stack);
  final int index, durationMs;
  final String status;
  final String? message, stack;
  factory Attempt.fromJson(Map<String, dynamic> j) {
    final error = j['error'] == null ? null : object(j['error']);
    return Attempt(j['attempt'] as int, j['status'] as String, j['durationMs'] as int,
        error?['message'] as String?, error?['stack'] as String?);
  }
}

class ResultDetail {
  const ResultDetail(this.result, this.attempts, this.events);
  final TestResult result;
  final List<Attempt> attempts;
  final List<TriageEvent> events;
  factory ResultDetail.fromJson(Map<String, dynamic> j) => ResultDetail(TestResult.fromJson(object(j['result'])),
      (j['attempts'] as List).map((e) => Attempt.fromJson(object(e))).toList(),
      (j['events'] as List).map((e) => TriageEvent.fromJson(object(e))).toList());
}

class TriageEvent {
  const TriageEvent(this.type, this.actor, this.at);
  final String type, actor;
  final DateTime at;
  factory TriageEvent.fromJson(Map<String, dynamic> j) => TriageEvent(j['type'] as String,
      j['actorName'] as String, DateTime.parse(j['occurredAt'] as String));
}

class Identity {
  const Identity(this.id, this.name, this.file, this.verdict, this.score, this.totalRuns, this.history);
  final String id, name, file, verdict;
  final double? score;
  final int totalRuns;
  final List<String> history;
  factory Identity.fromJson(Map<String, dynamic> j) => Identity(j['id'] as String, j['canonicalTitle'] as String,
      j['canonicalFilePath'] as String, j['verdict'] as String, (j['flakyScore'] as num?)?.toDouble(),
      j['totalRuns'] as int, (j['recentStatuses'] as List).cast<String>());
}
