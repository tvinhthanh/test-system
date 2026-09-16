import 'package:flutter/material.dart';
import 'package:go_router/go_router.dart';
import '../../../shared/widgets.dart';
import '../data/reports_repository.dart';
import '../domain/models.dart';
import 'report_view_models.dart';

class RunsPage extends StatefulWidget {
  const RunsPage({required this.repository, required this.project, this.identity, super.key});
  final ReportsRepository repository;
  final String project;
  final String? identity;
  @override
  State<RunsPage> createState() => _RunsPageState();
}

class _RunsPageState extends State<RunsPage> {
  late final model = RunsViewModel(widget.repository, widget.project, identity: widget.identity)..refresh();
  @override
  void dispose() { model.dispose(); super.dispose(); }
  @override
  Widget build(BuildContext context) => Column(crossAxisAlignment: CrossAxisAlignment.stretch, children: [
    PageHeader(widget.identity == null ? 'Test runs' : 'Test history',
      subtitle: widget.identity == null ? 'Playwright / All environments' : widget.identity,
      actions: [IconButton(tooltip: 'Refresh runs', onPressed: model.refresh, icon: const Icon(Icons.refresh, size: 20))]),
    if (widget.identity == null) Padding(padding: const EdgeInsets.only(bottom: 20), child: Wrap(spacing: 12, runSpacing: 12, children: [
      SizedBox(width: 280, child: TextField(onSubmitted: (q) => model.filter(search: q.trim()),
        decoration: const InputDecoration(hintText: 'Search branch, commit or run', prefixIcon: Icon(Icons.search, size: 19)))),
      SizedBox(width: 165, child: DropdownButtonFormField<String>(initialValue: '', decoration: const InputDecoration(contentPadding: EdgeInsets.symmetric(horizontal: 12)),
        items: [for (final status in ['', 'running', 'passed', 'failed', 'error', 'cancelled']) DropdownMenuItem(value: status, child: Text(status.isEmpty ? 'All statuses' : readable(status)))],
        onChanged: (s) => model.filter(status: s))),
    ])),
    const Divider(height: 1),
    ResourceView(controller: model, retry: model.refresh, builder: (page) => Column(children: [
      if (page.items.isEmpty) const EmptyState('No runs found') else RunList(runs: page.items, project: widget.project),
      CursorControls(previous: model.hasPrevious ? model.previous : null, next: page.nextCursor == null ? null : model.next),
    ])),
  ]);
}

class RunList extends StatelessWidget {
  const RunList({required this.runs, required this.project, super.key});
  final List<Run> runs;
  final String project;
  @override
  Widget build(BuildContext context) => LayoutBuilder(builder: (context, bounds) {
    final wide = bounds.maxWidth > 760;
    return Column(children: [
      if (wide) Container(height: 42, color: const Color(0xFFF6F7F8), padding: const EdgeInsets.symmetric(horizontal: 16), child: const Row(children: [
        Expanded(flex: 4, child: Text('RUN / BRANCH', style: TextStyle(fontSize: 10, color: muted))),
        Expanded(flex: 3, child: Text('STATUS', style: TextStyle(fontSize: 10, color: muted))),
        SizedBox(width: 110, child: Text('RESULTS', style: TextStyle(fontSize: 10, color: muted))),
        SizedBox(width: 85, child: Text('DURATION', style: TextStyle(fontSize: 10, color: muted))),
        SizedBox(width: 95, child: Text('STARTED', style: TextStyle(fontSize: 10, color: muted))),
      ])),
      for (final run in runs) InkWell(onTap: () => context.go('/p/$project/runs/${run.id}'), child: Container(
        padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 18), decoration: const BoxDecoration(border: Border(bottom: BorderSide(color: line))),
        child: wide ? Row(children: [
          Expanded(flex: 4, child: _RunName(run)),
          Expanded(flex: 3, child: Padding(padding: const EdgeInsets.only(right: 12), child: StatusBadge(run.status, flakyCount: run.flakyCount))),
          SizedBox(width: 110, child: _ResultBar(run)),
          SizedBox(width: 85, child: Text(duration(run.durationMs), style: const TextStyle(fontSize: 12))),
          SizedBox(width: 95, child: Text(timestamp(run.startedAt), style: const TextStyle(fontSize: 12, color: muted))),
        ]) : Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
          _RunName(run), const SizedBox(height: 14), StatusBadge(run.status, flakyCount: run.flakyCount),
          const SizedBox(height: 12), Text('${run.total} tests  /  ${duration(run.durationMs)}  /  ${timestamp(run.startedAt)}', style: const TextStyle(fontSize: 12, color: muted)),
        ]))),
    ]);
  });
}

class _RunName extends StatelessWidget {
  const _RunName(this.run);
  final Run run;
  @override
  Widget build(BuildContext context) => Padding(padding: const EdgeInsets.only(right: 12), child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
    Text(run.branch, maxLines: 2, overflow: TextOverflow.ellipsis, style: const TextStyle(fontWeight: FontWeight.w600, fontSize: 13)),
    const SizedBox(height: 6), Text('${shortId(run.id)}  /  ${shortId(run.commit)}  /  ${run.environment}',
      maxLines: 2, overflow: TextOverflow.ellipsis, style: const TextStyle(fontSize: 11, color: muted)),
  ]));
}

class _ResultBar extends StatelessWidget {
  const _ResultBar(this.run);
  final Run run;
  @override
  Widget build(BuildContext context) => Padding(padding: const EdgeInsets.only(right: 22), child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
    Text('${run.total} tests', style: const TextStyle(fontSize: 11, color: muted)), const SizedBox(height: 7),
    Semantics(label: '${run.passed} passed, ${run.failed} failed, ${run.flakyCount} flaky', child: SizedBox(height: 4, child: Row(children: [
      if (run.passed > 0) Expanded(flex: run.passed, child: Container(color: green)),
      if (run.failed > 0) Expanded(flex: run.failed, child: Container(color: red)),
      if (run.flakyCount > 0) Expanded(flex: run.flakyCount, child: Container(color: amber)),
      if (run.total > run.passed + run.failed + run.flakyCount) Expanded(flex: run.total-run.passed-run.failed-run.flakyCount, child: Container(color: line)),
    ]))),
  ]));
}
