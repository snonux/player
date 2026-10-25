import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../providers/api_client_provider.dart';
import '../utils/error_mappers.dart';

/// Admin-only rescan screen.
///
/// Design notes:
///   - The user taps "Trigger Rescan" to start a library rescan on the server.
///   - After triggering, the screen polls [getScanProgress] every 2 seconds
///     while the scan is running, displaying live progress (file counts, current
///     set name).
///   - The poll timer is stored in [_pollTimer] and cancelled in [dispose] to
///     prevent memory leaks and spurious setState calls after the widget is gone.
///   - A generation counter prevents stale polling results from overwriting
///     the state after the user navigates away and back again.
///   - The trigger button is disabled while a scan is actively running to
///     prevent duplicate scans.
///   - All async continuations guard on [mounted] to prevent setState/context
///     calls after widget disposal.
class AdminRescanScreen extends ConsumerStatefulWidget {
  const AdminRescanScreen({super.key});

  @override
  ConsumerState<AdminRescanScreen> createState() => _AdminRescanScreenState();
}

class _AdminRescanScreenState extends ConsumerState<AdminRescanScreen> {
  // Interval between progress poll requests while a scan is running.
  static const _pollInterval = Duration(seconds: 2);

  // Null before the first status fetch, non-null after.
  _ScanStatus? _status;

  // Non-null when the last API call failed.
  String? _error;

  // True while the trigger request is in flight.
  bool _isTriggering = false;

  bool _fetching = false;

  // Active polling timer; cancelled in dispose and whenever the scan finishes.
  Timer? _pollTimer;

  // Generation counter: async completions discard results if they captured a
  // stale generation value (prevents out-of-order result clobbering).
  int _generation = 0;

  @override
  void initState() {
    super.initState();
    // Fetch the current scan status immediately so the user sees whether a
    // scan is already running (e.g. started by another admin session).
    WidgetsBinding.instance.addPostFrameCallback((_) => _fetchStatus());
  }

  @override
  void dispose() {
    // Always cancel the polling timer to avoid calling setState after disposal
    // and to release the periodic timer resource.
    _pollTimer?.cancel();
    super.dispose();
  }

  // ---------------------------------------------------------------------------
  // Status fetching
  // ---------------------------------------------------------------------------

  /// Fetches the current scan progress and updates [_status].
  ///
  /// If the scan is running, a poll timer is started (or kept running).
  /// If the scan is idle/complete, any active poll timer is cancelled.
  Future<void> _fetchStatus() async {
    if (!mounted || _fetching) return;
    _fetching = true;
    final generation = ++_generation;

    try {
      final raw = await ref.read(apiClientProvider).getScanProgress();
      if (!mounted || generation != _generation) return;

      final status = _ScanStatus.fromMap(raw);
      setState(() {
        _status = status;
        _error = null;
      });

      _updatePolling(status.isRunning ||
          status.watchSeconds > 0 ||
          status.intervalMinutes > 0);
    } catch (e) {
      if (!mounted || generation != _generation) return;
      setState(() => _error = adminRescanErrorMessage(e));
      // Stop polling on error to avoid hammering a broken endpoint; the user
      // can retry manually via the refresh button.
      _pollTimer?.cancel();
      _pollTimer = null;
    } finally {
      _fetching = false;
    }
  }

  /// Starts or stops the background polling timer based on [scanRunning].
  ///
  /// Starts a new periodic timer when [scanRunning] is true and no timer is
  /// active; cancels any active timer when [scanRunning] is false.
  void _updatePolling(bool scanRunning) {
    if (scanRunning && _pollTimer == null) {
      // Poll every 2 seconds while the scan is running to show live progress.
      _pollTimer = Timer.periodic(_pollInterval, (_) => _fetchStatus());
    } else if (!scanRunning) {
      _pollTimer?.cancel();
      _pollTimer = null;
    }
  }

  // ---------------------------------------------------------------------------
  // Trigger rescan action
  // ---------------------------------------------------------------------------

  /// Sends a trigger-rescan request and immediately begins polling for progress.
  Future<void> _triggerRescan() async {
    if (!mounted || _isTriggering) return;
    setState(() {
      _isTriggering = true;
      _error = null;
    });

    try {
      await ref.read(apiClientProvider).triggerRescan();
      if (!mounted) return;
      setState(() => _isTriggering = false);
      // Start polling immediately so the user sees progress as soon as the
      // server reports the scan has begun.
      await _fetchStatus();
    } catch (e) {
      if (!mounted) return;
      setState(() {
        _isTriggering = false;
        _error = adminRescanErrorMessage(e);
      });
    }
  }

  Future<void> _cancelScan() async {
    if (_isTriggering) return;
    setState(() {
      _isTriggering = true;
      _error = null;
    });
    try {
      await ref.read(apiClientProvider).cancelScan();
      if (mounted) await _fetchStatus();
    } catch (e) {
      if (mounted) setState(() => _error = adminRescanErrorMessage(e));
    } finally {
      if (mounted) setState(() => _isTriggering = false);
    }
  }

  // ---------------------------------------------------------------------------
  // Build
  // ---------------------------------------------------------------------------

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: const Text('Rescan Library'),
        actions: [
          IconButton(
            key: const Key('admin_rescan_refresh'),
            icon: const Icon(Icons.refresh),
            tooltip: 'Check status',
            onPressed: _fetchStatus,
          ),
        ],
      ),
      body: SafeArea(
        child: Padding(
          padding: const EdgeInsets.all(24),
          child: _buildBody(context),
        ),
      ),
    );
  }

  /// Builds the screen body: status card + trigger button.
  Widget _buildBody(BuildContext context) {
    return ListView(
      children: [
        _StatusCard(status: _status, error: _error),
        const SizedBox(height: 32),
        _TriggerButton(
          isRunning: _status?.isRunning ?? false,
          isTriggering: _isTriggering,
          onTap: _triggerRescan,
        ),
        if (_status?.isRunning ?? false) ...[
          const SizedBox(height: 12),
          OutlinedButton.icon(
            key: const Key('admin_scan_cancel'),
            onPressed: _isTriggering || _status?.state == 'cancelling'
                ? null
                : _cancelScan,
            icon: const Icon(Icons.stop_circle_outlined),
            label: Text(_status?.state == 'cancelling'
                ? 'Cancelling scan…'
                : 'Cancel scan'),
          ),
        ],
        if (_status?.pending ?? false) const Text('One follow-up scan queued.'),
        if (_status?.watchError.isNotEmpty ?? false)
          Text(_status!.watchError,
              style: TextStyle(color: Theme.of(context).colorScheme.error)),
        const SizedBox(height: 16),
        ExpansionTile(
          title: const Text('Scan settings'),
          childrenPadding: const EdgeInsets.all(16),
          children: [
            Text(_status == null
                ? 'Loading effective settings…'
                : 'Watch: ${_status!.watchSeconds > 0 ? 'polling every ${_status!.watchSeconds} seconds' : 'Off'}. '
                    'Schedule: ${_status!.intervalMinutes > 0 ? 'every ${_status!.intervalMinutes} minutes' : 'Off'}.'),
            const SizedBox(height: 12),
            const SelectableText('Configure on the server, then restart:\n'
                'SCAN_WATCH_SECONDS=30\nSCAN_INTERVAL_MINUTES=60\n'
                'Both default to 0 (off). Watch supports 10–86400 seconds; schedule supports 1–10080 minutes. '
                'Manage roots in Libraries. Polling checks file names, size and modification time; '
                'same-size edits with unchanged time need a manual rescan. '
                'Automatic scans retain absent records; manual rescans use the existing deletion policy. '
                'Generated caches are ignored. Back leaves scans running; Cancel scan stops active and queued work.'),
          ],
        ),
      ],
    );
  }
}

// ---------------------------------------------------------------------------
// Data model for scan progress
// ---------------------------------------------------------------------------

/// Parsed scan progress state returned by GET /api/v1/admin/scan-progress.
///
/// Kept as a plain data class (no business logic) so [_AdminRescanScreenState]
/// and the sub-widgets stay focused on their own concerns (SRP).
class _ScanStatus {
  const _ScanStatus({
    required this.isRunning,
    required this.currentSet,
    required this.setsTotal,
    required this.setsDone,
    required this.filesTotal,
    required this.filesDone,
    this.lastError,
    this.state = '',
    this.trigger = '',
    this.pending = false,
    this.watchSeconds = 0,
    this.intervalMinutes = 0,
    this.watchError = '',
  });

  /// Parses the raw progress map returned by the server.
  factory _ScanStatus.fromMap(Map<String, dynamic> map) {
    return _ScanStatus(
      isRunning: map['running'] as bool? ?? false,
      currentSet: map['current_set'] as String? ?? '',
      setsTotal: map['sets_total'] as int? ?? 0,
      setsDone: map['sets_done'] as int? ?? 0,
      filesTotal: map['files_total'] as int? ?? 0,
      filesDone: map['files_done'] as int? ?? 0,
      lastError: map['last_error'] as String?,
      state: map['state'] as String? ?? '',
      trigger: map['trigger'] as String? ?? '',
      pending: map['pending'] as bool? ?? false,
      watchSeconds: map['watch_seconds'] as int? ?? 0,
      intervalMinutes: map['interval_minutes'] as int? ?? 0,
      watchError: map['watch_error'] as String? ?? '',
    );
  }

  final bool isRunning;
  final String currentSet;
  final int setsTotal;
  final int setsDone;
  final int filesTotal;
  final int filesDone;
  final String? lastError;
  final String state, trigger, watchError;
  final bool pending;
  final int watchSeconds, intervalMinutes;
}

// ---------------------------------------------------------------------------
// Sub-widgets
// ---------------------------------------------------------------------------

/// Card that displays the current scan status and progress.
///
/// Shows a spinner + live counters while running; shows "Idle" or "Scan
/// complete" when not running; shows a loading placeholder before the first
/// status fetch completes.
class _StatusCard extends StatelessWidget {
  const _StatusCard({required this.status, required this.error});

  final _ScanStatus? status;
  final String? error;

  @override
  Widget build(BuildContext context) {
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(20),
        child: _cardContent(context),
      ),
    );
  }

  /// Returns the inner content of the status card.
  Widget _cardContent(BuildContext context) {
    // Show error state if there was an API failure.
    if (error != null) {
      return _ErrorRow(message: error!);
    }

    // Show a spinner while the initial status fetch is in progress.
    final s = status;
    if (s == null) {
      return const Center(
        key: Key('admin_rescan_status_loading'),
        child: CircularProgressIndicator(),
      );
    }

    if (s.isRunning) {
      return _RunningContent(status: s);
    }

    return _IdleContent(status: s);
  }
}

/// Status card content while a scan is running.
class _RunningContent extends StatelessWidget {
  const _RunningContent({required this.status});

  final _ScanStatus status;

  @override
  Widget build(BuildContext context) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      mainAxisSize: MainAxisSize.min,
      children: [
        Row(
          children: [
            const SizedBox(
              width: 20,
              height: 20,
              child: CircularProgressIndicator(strokeWidth: 2),
            ),
            const SizedBox(width: 12),
            Text(
              status.state == 'cancelling'
                  ? 'Cancelling scan…'
                  : 'Scan running…',
              key: const Key('admin_rescan_running_label'),
              style: Theme.of(context).textTheme.titleSmall,
            ),
          ],
        ),
        if (status.currentSet.isNotEmpty) ...[
          const SizedBox(height: 12),
          Text(
            'Current set: ${status.currentSet}',
            style: Theme.of(context).textTheme.bodyMedium,
          ),
        ],
        if (status.trigger.isNotEmpty) Text('Started by: ${status.trigger}'),
        if (status.setsTotal > 0) ...[
          const SizedBox(height: 6),
          Text('Sets: ${status.setsDone} / ${status.setsTotal}'),
        ],
        if (status.filesTotal > 0) ...[
          const SizedBox(height: 6),
          Text('Files: ${status.filesDone} / ${status.filesTotal}'),
        ],
      ],
    );
  }
}

/// Status card content when no scan is running.
class _IdleContent extends StatelessWidget {
  const _IdleContent({required this.status});

  final _ScanStatus status;

  @override
  Widget build(BuildContext context) {
    // Show a "Scan complete" summary when there are files already scanned;
    // otherwise show the neutral "Idle" state.
    final hasScanned = status.filesTotal > 0 || status.setsDone > 0;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      mainAxisSize: MainAxisSize.min,
      children: [
        Row(
          children: [
            Icon(
              hasScanned ? Icons.check_circle_outline : Icons.schedule_outlined,
              color: hasScanned
                  ? Theme.of(context).colorScheme.primary
                  : Theme.of(context).colorScheme.onSurfaceVariant,
            ),
            const SizedBox(width: 12),
            Text(
              status.state == 'cancelled'
                  ? 'Scan cancelled'
                  : status.state == 'failed' ||
                          (status.lastError?.isNotEmpty ?? false)
                      ? 'Scan failed'
                      : hasScanned
                          ? 'Scan complete'
                          : 'Idle — no scan running',
              key: const Key('admin_rescan_idle_label'),
              style: Theme.of(context).textTheme.titleSmall,
            ),
          ],
        ),
        if (hasScanned && status.filesTotal > 0) ...[
          const SizedBox(height: 8),
          Text('Files scanned: ${status.filesDone} / ${status.filesTotal}'),
        ],
        if (status.lastError != null && status.lastError!.isNotEmpty) ...[
          const SizedBox(height: 8),
          Text(
            status.state == 'cancelled'
                ? 'Rescan when ready.'
                : 'Check library root access and server logs, then trigger rescan.',
            style: TextStyle(color: Theme.of(context).colorScheme.error),
          ),
        ],
      ],
    );
  }
}

/// Inline error row shown inside the status card.
class _ErrorRow extends StatelessWidget {
  const _ErrorRow({required this.message});

  final String message;

  @override
  Widget build(BuildContext context) {
    return Row(
      children: [
        Icon(Icons.error_outline, color: Theme.of(context).colorScheme.error),
        const SizedBox(width: 12),
        Expanded(
          child: Text(
            message,
            key: const Key('admin_rescan_error'),
            style: TextStyle(color: Theme.of(context).colorScheme.error),
          ),
        ),
      ],
    );
  }
}

/// Button that triggers a rescan.
///
/// Disabled while a scan is running or a trigger request is in flight,
/// preventing duplicate scans and accidental double-taps.
class _TriggerButton extends StatelessWidget {
  const _TriggerButton({
    required this.isRunning,
    required this.isTriggering,
    required this.onTap,
  });

  final bool isRunning;
  final bool isTriggering;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    // Disable the button while a scan is active or the trigger is in flight.
    final canTrigger = !isRunning && !isTriggering;

    return FilledButton.icon(
      key: const Key('admin_rescan_trigger'),
      onPressed: canTrigger ? onTap : null,
      icon: isTriggering
          ? const SizedBox(
              width: 18,
              height: 18,
              child: CircularProgressIndicator(
                  strokeWidth: 2, color: Colors.white),
            )
          : const Icon(Icons.sync_outlined),
      label: Text(isRunning ? 'Scan in progress…' : 'Trigger Rescan'),
    );
  }
}
