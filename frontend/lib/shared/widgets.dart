import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import '../core/api/api_client.dart';
import '../core/state/resource_controller.dart';

const ink = Color(0xFF22272B);
const muted = Color(0xFF687078);
const line = Color(0xFFE2E5E7);
const green = Color(0xFF187650);
const amber = Color(0xFFA4640B);
const red = Color(0xFFBC3B46);

String readable(String value) => value.replaceAll('_', ' ');
String shortId(String id) => id.length > 10 ? id.substring(0, 10) : id;
String duration(int? ms) => ms == null ? 'In progress' : ms < 60000 ? '${(ms / 1000).toStringAsFixed(1)}s' : '${ms ~/ 60000}m ${(ms % 60000) ~/ 1000}s';
String timestamp(DateTime date) {
  final local = date.toLocal();
  return '${local.day.toString().padLeft(2, '0')}/${local.month.toString().padLeft(2, '0')} ${local.hour.toString().padLeft(2, '0')}:${local.minute.toString().padLeft(2, '0')}';
}

Color statusColor(String status) => switch (status) {
  'passed' || 'stable' => green,
  'flaky_pass' || 'flaky' || 'suspect' || 'warning' || 'quarantine_suggested' => amber,
  'failed' || 'error' || 'timed_out' || 'interrupted' => red,
  'running' || 'queued' => const Color(0xFF286DAB),
  _ => muted,
};

class StatusBadge extends StatelessWidget {
  const StatusBadge(this.status, {this.flakyCount = 0, super.key});
  final String status;
  final int flakyCount;
  @override
  Widget build(BuildContext context) {
    final warning = status == 'passed' && flakyCount > 0;
    final color = statusColor(warning ? 'warning' : status);
    final label = warning ? 'Passed with flaky tests ($flakyCount)' : readable(status);
    return Semantics(label: label, child: Row(mainAxisSize: MainAxisSize.min, children: [
      Icon(warning || status == 'flaky_pass' ? Icons.warning_amber_rounded : Icons.circle, size: 12, color: color),
      const SizedBox(width: 6), Flexible(child: Text(label, style: TextStyle(color: color, fontWeight: FontWeight.w600, fontSize: 12))),
    ]));
  }
}

class PageHeader extends StatelessWidget {
  const PageHeader(this.title, {this.subtitle, this.actions = const [], super.key});
  final String title;
  final String? subtitle;
  final List<Widget> actions;
  @override
  Widget build(BuildContext context) => Padding(padding: const EdgeInsets.only(bottom: 24), child: Row(children: [
    Expanded(child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
      Text(title, style: const TextStyle(fontSize: 26, fontWeight: FontWeight.w700)),
      if (subtitle != null) ...[const SizedBox(height: 6), Text(subtitle!, style: const TextStyle(color: muted, fontSize: 13))],
    ])), ...actions,
  ]));
}

class ResourceView<T> extends StatelessWidget {
  const ResourceView({required this.controller, required this.retry, required this.builder, super.key});
  final ResourceController<T> controller;
  final VoidCallback retry;
  final Widget Function(T) builder;
  @override
  Widget build(BuildContext context) => ListenableBuilder(listenable: controller, builder: (context, _) {
    if (controller.loading) return const Padding(padding: EdgeInsets.all(48), child: Center(child: CircularProgressIndicator(strokeWidth: 2)));
    if (controller.error != null) return FailureState(controller.error!, retry: retry);
    final data = controller.data;
    if (data == null) return const SizedBox.shrink();
    return builder(data);
  });
}

class FailureState extends StatelessWidget {
  const FailureState(this.error, {required this.retry, super.key});
  final Object error;
  final VoidCallback retry;
  @override
  Widget build(BuildContext context) => Padding(padding: const EdgeInsets.symmetric(vertical: 48), child: Column(children: [
    const Icon(Icons.cloud_off_outlined, color: muted, size: 32), const SizedBox(height: 16),
    Text(error is ApiException ? (error as ApiException).message : 'The data could not be loaded.', textAlign: TextAlign.center),
    if (error is ApiException && (error as ApiException).requestId != null)
      SelectableText('Request ${(error as ApiException).requestId}', style: const TextStyle(color: muted)),
    const SizedBox(height: 12), TextButton.icon(onPressed: retry, icon: const Icon(Icons.refresh), label: const Text('Try again')),
  ]));
}

class EmptyState extends StatelessWidget {
  const EmptyState(this.title, {super.key});
  final String title;
  @override
  Widget build(BuildContext context) => Padding(padding: const EdgeInsets.all(48), child: Center(child: Column(children: [
    const Icon(Icons.inbox_outlined, size: 36, color: muted), const SizedBox(height: 16), Text(title, textAlign: TextAlign.center),
  ])));
}

class TechnicalBlock extends StatelessWidget {
  const TechnicalBlock(this.title, this.text, {super.key});
  final String title, text;
  @override
  Widget build(BuildContext context) => Container(margin: const EdgeInsets.only(bottom: 16),
    decoration: BoxDecoration(color: const Color(0xFFF6F7F8), border: Border.all(color: line), borderRadius: BorderRadius.circular(6)),
    child: Column(crossAxisAlignment: CrossAxisAlignment.stretch, children: [
      Padding(padding: const EdgeInsets.only(left: 16, right: 6), child: Row(children: [
        Expanded(child: Text(title, style: const TextStyle(fontWeight: FontWeight.w600, fontSize: 12))),
        IconButton(tooltip: 'Copy $title', icon: const Icon(Icons.copy_outlined, size: 16), onPressed: () async {
          try {
            await Clipboard.setData(ClipboardData(text: text));
            if (context.mounted) ScaffoldMessenger.of(context).showSnackBar(const SnackBar(content: Text('Copied')));
          } catch (_) {
            if (context.mounted) ScaffoldMessenger.of(context).showSnackBar(const SnackBar(content: Text('Copy failed. Select the text to copy it.')));
          }
        }),
      ])), const Divider(height: 1),
      Padding(padding: const EdgeInsets.all(16), child: SelectionArea(child: Text(text,
        style: const TextStyle(fontFamily: 'monospace', fontSize: 12, height: 1.7)))),
    ]));
}

class HistoryStrip extends StatelessWidget {
  const HistoryStrip(this.statuses, {super.key});
  final List<String> statuses;
  @override
  Widget build(BuildContext context) => Wrap(spacing: 3, runSpacing: 3, children: [
    for (var i = 0; i < statuses.length; i++) Tooltip(message: 'Run ${i + 1}: ${readable(statuses[i])}',
      child: Container(width: 7, height: 22, decoration: BoxDecoration(color: statusColor(statuses[i]), borderRadius: BorderRadius.circular(2)))),
  ]);
}

class CursorControls extends StatelessWidget {
  const CursorControls({required this.previous, required this.next, super.key});
  final VoidCallback? previous, next;
  @override
  Widget build(BuildContext context) => Row(mainAxisAlignment: MainAxisAlignment.end, children: [
    IconButton(tooltip: 'Previous page', onPressed: previous, icon: const Icon(Icons.chevron_left)),
    IconButton(tooltip: 'Next page', onPressed: next, icon: const Icon(Icons.chevron_right)),
  ]);
}
