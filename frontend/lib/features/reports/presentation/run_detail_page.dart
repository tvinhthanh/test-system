import 'package:flutter/material.dart';
import 'package:go_router/go_router.dart';
import '../../../shared/widgets.dart';
import '../data/reports_repository.dart';
import 'report_view_models.dart';

class RunDetailPage extends StatefulWidget {
  const RunDetailPage({required this.repository, required this.project, required this.id, super.key});
  final ReportsRepository repository;
  final String project, id;
  @override
  State<RunDetailPage> createState() => _RunDetailPageState();
}

class _RunDetailPageState extends State<RunDetailPage> {
  late final model = RunDetailViewModel(widget.repository, widget.project, widget.id)..refresh();
  @override
  void dispose() { model.dispose(); super.dispose(); }
  @override
  Widget build(BuildContext context) => Column(crossAxisAlignment: CrossAxisAlignment.stretch, children: [
    Align(alignment: Alignment.centerLeft, child: TextButton.icon(onPressed: () => context.go('/p/${widget.project}/runs'),
      icon: const Icon(Icons.arrow_back, size: 16), label: const Text('Test runs'))),
    const SizedBox(height: 12), PageHeader('Run ${shortId(widget.id)}', actions: [IconButton(tooltip: 'Refresh run', onPressed: model.refresh, icon: const Icon(Icons.refresh))]),
    ResourceView(controller: model, retry: model.refresh, builder: (data) => Column(crossAxisAlignment: CrossAxisAlignment.stretch, children: [
      StatusBadge(data.run.status, flakyCount: data.run.flakyCount), const SizedBox(height: 16),
      SelectionArea(child: Text('${data.run.branch} / ${data.run.commit} / ${data.run.environment}', style: const TextStyle(color: muted))),
      const SizedBox(height: 24), Wrap(spacing: 40, runSpacing: 16, children: [
        _Metric('Tests', '${data.run.total}', ink), _Metric('Passed', '${data.run.passed}', green),
        _Metric('Failed', '${data.run.failed}', red), _Metric('Flaky', '${data.run.flakyCount}', amber),
        _Metric('Duration', duration(data.run.durationMs), ink),
      ]), const SizedBox(height: 28), const Divider(height: 1),
      if (data.results.items.isEmpty) const EmptyState('No test results yet'),
      for (final result in data.results.items) InkWell(onTap: () => context.go('/p/${widget.project}/runs/${widget.id}/results/${result.id}'),
        child: Container(padding: const EdgeInsets.symmetric(vertical: 18), decoration: const BoxDecoration(border: Border(bottom: BorderSide(color: line))),
          child: Row(children: [
            Icon(result.status == 'passed' ? Icons.check_circle_outline : Icons.error_outline, color: statusColor(result.status), size: 20),
            const SizedBox(width: 14), Expanded(child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
              Text(result.name, style: const TextStyle(fontWeight: FontWeight.w600, fontSize: 13)), const SizedBox(height: 6),
              Text('${result.file} / ${result.attempts} attempts / ${duration(result.durationMs)}', style: const TextStyle(color: muted, fontSize: 11)),
              const SizedBox(height: 6), StatusBadge(result.status),
            ])), const Icon(Icons.chevron_right, color: muted, size: 18),
          ]))),
      CursorControls(previous: model.hasPrevious ? model.previous : null, next: data.results.nextCursor == null ? null : model.next),
    ])),
  ]);
}

class _Metric extends StatelessWidget {
  const _Metric(this.label, this.value, this.color);
  final String label, value;
  final Color color;
  @override
  Widget build(BuildContext context) => Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
    Text(label, style: const TextStyle(fontSize: 12, color: muted)), const SizedBox(height: 8),
    Text(value, style: TextStyle(fontSize: 25, fontWeight: FontWeight.w600, color: color)),
  ]);
}
