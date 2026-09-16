import '../../../core/api/api_client.dart';
import '../domain/models.dart';

/// Dashboard read models belong to backend query; these are proposed HTTP routes.
class ReportsRepository {
  ReportsRepository(this.api);
  final ApiClient api;
  String scope(String project) => '/v1/projects/${Uri.encodeComponent(project)}';
  Future<List<Project>> projects() async =>
      ((await api.get('/v1/projects'))['items'] as List).map((j) => Project.fromJson(object(j))).toList();
  Future<PageResult<Run>> runs(String project, {String status = '', String search = '', String? cursor}) async =>
      PageResult.fromJson(await api.get('${scope(project)}/runs', {
        'limit': '20', if (status.isNotEmpty) 'status': status,
        if (search.isNotEmpty) 'q': search, if (cursor != null) 'cursor': cursor,
      }), Run.fromJson);
  Future<Run> run(String project, String id) async => Run.fromJson(
      await api.get('${scope(project)}/runs/${Uri.encodeComponent(id)}'));
  Future<PageResult<TestResult>> results(String project, String run, {String? cursor}) async =>
      PageResult.fromJson(await api.get('${scope(project)}/runs/${Uri.encodeComponent(run)}/results',
          {'limit': '50', if (cursor != null) 'cursor': cursor}), TestResult.fromJson);
  Future<ResultDetail> result(String project, String id) async => ResultDetail.fromJson(
      await api.get('${scope(project)}/results/${Uri.encodeComponent(id)}'));
  Future<PageResult<Identity>> identities(String project, {bool flaky = false, String? cursor}) async =>
      PageResult.fromJson(await api.get('${scope(project)}/identities', {
        'limit': '20', if (flaky) 'view': 'flaky', if (cursor != null) 'cursor': cursor,
      }), Identity.fromJson);
  Future<PageResult<Run>> history(String project, String id, {String? cursor}) async =>
      PageResult.fromJson(await api.get('${scope(project)}/identities/${Uri.encodeComponent(id)}/runs',
          {'limit': '20', if (cursor != null) 'cursor': cursor}), Run.fromJson);
  Future<void> triage(String project, String result, String type) async {
    await api.post('${scope(project)}/results/${Uri.encodeComponent(result)}/triage-events', {'type': type, 'payload': <String, Object?>{}});
  }
}
