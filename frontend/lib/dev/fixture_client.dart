import 'dart:convert';
import 'package:http/http.dart' as http;

/// Explicit preview-only transport. Live failures never fall back to fixtures.
class FixtureClient extends http.BaseClient {
  bool signedIn = true;
  final Map<String, List<Map<String, Object?>>> events = {};
  final now = DateTime.utc(2026, 9, 14, 9, 42);
  Map<String, Object?> get user => {'name': 'Alex Morgan', 'email': 'alex@example.test'};

  Map<String, Object?> run(int i) => {
    'id': 'run-${1048 - i}', 'branch': i % 3 == 0 ? 'feat/checkout-validation' : 'main',
    'commitSha': ['a72f91c', 'f803b2d', '9be420a'][i % 3],
    'status': i == 0 ? 'running' : i % 4 == 1 ? 'failed' : 'passed',
    'environmentName': i % 3 == 0 ? 'Preview' : 'Staging',
    'startedAt': now.subtract(Duration(minutes: i * 37)).toIso8601String(),
    'durationMs': i == 0 ? null : 92400 + i * 2700,
    'totalCount': 128, 'passedCount': i == 0 ? 83 : i % 4 == 1 ? 121 : i % 3 == 0 ? 126 : 128,
    'failedCount': i % 4 == 1 ? 5 : 0, 'flakyCount': i % 4 == 1 || i % 3 == 0 ? 2 : 0,
  };
  static const names = ['Checkout completes with a saved card', 'Guest can apply a discount code',
    'Order confirmation shows delivery date', 'User signs in with valid credentials',
    'Cart preserves quantities after refresh', 'Search returns matching products',
    'Payment rejects an expired card', 'Account settings persist across sessions'];
  Map<String, Object?> result(int i) => {
    'id': 'result-$i', 'stableTestId': i == 2 ? null : 'test-$i', 'displayName': names[i],
    'filePath': i < 3 ? 'tests/checkout/checkout.spec.ts' : 'tests/account/account.spec.ts',
    'finalStatus': i == 0 ? 'failed' : i == 1 ? 'flaky_pass' : 'passed',
    'attemptCount': i < 2 ? 2 : 1, 'durationMs': 1200 + i * 540,
  };
  Map<String, Object?> identity(int i) => {
    'id': 'test-$i', 'canonicalTitle': names[i],
    'canonicalFilePath': 'tests/${i < 3 ? 'checkout/checkout' : 'account/account'}.spec.ts',
    'verdict': i == 0 ? 'flaky' : i == 1 ? 'suspect' : i == 7 ? 'insufficient_data' : 'stable',
    'flakyScore': i == 0 ? 0.72 : i == 1 ? 0.38 : i == 7 ? null : 0.04,
    'totalRuns': i == 7 ? 1 : 30,
    'recentStatuses': List.generate(i == 7 ? 1 : 18, (n) => i < 2 && n % 4 == 0 ? 'flaky_pass' : n == 16 && i == 0 ? 'failed' : 'passed'),
  };

  @override
  Future<http.StreamedResponse> send(http.BaseRequest request) async {
    await Future<void>.delayed(const Duration(milliseconds: 180));
    final path = request.url.path;
    Object response;
    var status = 200;
    if (path == '/v1/session/logout') {
      signedIn = false;
      response = {};
    } else if (path == '/v1/session') {
      if (request.method == 'POST') signedIn = true;
      response = {'user': signedIn ? user : null, 'csrfToken': 'fixture-only-csrf'};
    } else if (!signedIn) {
      status = 401;
      response = {'error': {'code': 'UNAUTHENTICATED'}};
    } else if (path == '/v1/projects') {
      response = {'items': [{'id': 'commerce', 'name': 'Commerce Web', 'workspaceName': 'Acme Engineering'},
        {'id': 'platform', 'name': 'Platform API', 'workspaceName': 'Acme Engineering'}]};
    } else if (path.contains('/projects/platform/')) {
      response = {'items': [], 'nextCursor': null};
    } else if (path.endsWith('/triage-events')) {
      final id = path.split('/')[5];
      final payload = jsonDecode((request as http.Request).body) as Map<String, dynamic>;
      final list = events.putIfAbsent(id, () => []);
      if (payload['type'] != 'FAILURE_SEEN' || !list.any((e) => e['type'] == 'FAILURE_SEEN')) {
        list.add({'type': payload['type'], 'actorName': 'Alex Morgan', 'occurredAt': DateTime.now().toUtc().toIso8601String()});
      }
      response = {};
    } else if (RegExp(r'/results/result-\d+$').hasMatch(path)) {
      final index = int.parse(path.split('-').last);
      response = {'result': result(index), 'attempts': List.generate(index < 2 ? 2 : 1, (a) => {
        'attempt': a, 'status': index == 0 || (index == 1 && a == 0) ? 'failed' : 'passed', 'durationMs': 1200,
        'error': index < 2 && (index == 0 || a == 0) ? {
          'message': 'Error: expect(locator).toBeVisible()\n\nLocator: getByRole("heading", { name: "Order confirmed" })\nExpected: visible\nReceived: element not found\nTimeout: 5000ms',
          'stack': 'at tests/checkout/checkout.spec.ts:42:31\n\n40 | await page.getByRole("button", { name: "Place order" }).click();\n41 |\n42 | await expect(page.getByRole("heading", { name: "Order confirmed" }))\n43 |   .toBeVisible();',
        } : null,
      }), 'events': events[path.split('/').last] ?? []};
    } else if (path.endsWith('/results')) {
      response = {'items': List.generate(8, result), 'nextCursor': null};
    } else if (path.contains('/runs/run-')) {
      response = run(1048 - int.parse(path.split('-').last));
    } else if (path.endsWith('/runs')) {
      var items = List.generate(18, run);
      final filter = request.url.queryParameters['status'];
      final q = request.url.queryParameters['q']?.toLowerCase() ?? '';
      items = items.where((r) => (filter == null || r['status'] == filter) &&
        '${r['branch']} ${r['commitSha']} ${r['id']}'.toLowerCase().contains(q)).toList();
      final offset = int.tryParse(request.url.queryParameters['cursor'] ?? '') ?? 0;
      response = {'items': items.skip(offset).take(10).toList(), 'nextCursor': offset + 10 < items.length ? '${offset + 10}' : null};
    } else if (path.endsWith('/identities')) {
      response = {'items': List.generate(request.url.queryParameters['view'] == 'flaky' ? 2 : 8, identity), 'nextCursor': null};
    } else {
      status = 404;
      response = {'error': {'code': 'NOT_FOUND'}};
    }
    return http.StreamedResponse(Stream.value(utf8.encode(jsonEncode(response))), status,
        headers: {'content-type': 'application/json'});
  }
}
