package zone.foo.player_android

import android.app.PendingIntent
import android.appwidget.AppWidgetManager
import android.appwidget.AppWidgetProvider
import android.content.ComponentName
import android.content.Context
import android.content.Intent
import android.os.Bundle
import android.view.KeyEvent
import android.view.View
import android.widget.RemoteViews

class PlaybackWidgetProvider : AppWidgetProvider() {
    override fun onUpdate(context: Context, manager: AppWidgetManager, ids: IntArray) = refresh(context)

    override fun onAppWidgetOptionsChanged(context: Context, manager: AppWidgetManager, id: Int, options: Bundle) = refresh(context)

    override fun onReceive(context: Context, intent: Intent) {
        super.onReceive(context, intent)
        val command = intent.action?.removePrefix(ACTION_PREFIX)
        val key = when (command) {
            "previous" -> KeyEvent.KEYCODE_MEDIA_PREVIOUS
            "next" -> KeyEvent.KEYCODE_MEDIA_NEXT
            "toggle" -> if (PlaybackWidgetState.enabled("playing")) KeyEvent.KEYCODE_MEDIA_PAUSE else KeyEvent.KEYCODE_MEDIA_PLAY
            else -> return
        }
        if (!PlaybackWidgetState.enabled(command!!)) {
            // Cold process or failed/ended session: require explicit app playback.
            // Never replay saved credentials or start a stale queue from a widget.
            refresh(context)
            if (PlaybackWidgetState.text("status").isEmpty()) context.startActivity(openIntent(context))
            return
        }
        context.sendBroadcast(Intent(Intent.ACTION_MEDIA_BUTTON).apply {
            component = ComponentName(context, com.ryanheise.audioservice.MediaButtonReceiver::class.java)
            putExtra(Intent.EXTRA_KEY_EVENT, KeyEvent(KeyEvent.ACTION_DOWN, key))
        })
    }

    companion object {
        private const val ACTION_PREFIX = "zone.foo.player_android.widget."

        private fun openIntent(context: Context) = Intent(context, MainActivity::class.java)
            .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_CLEAR_TOP or Intent.FLAG_ACTIVITY_SINGLE_TOP)

        fun refresh(context: Context) {
            val manager = AppWidgetManager.getInstance(context)
            val ids = manager.getAppWidgetIds(ComponentName(context, PlaybackWidgetProvider::class.java))
            for (id in ids) {
                val views = RemoteViews(context.packageName, R.layout.playback_widget)
                val state = PlaybackWidgetState
                val empty = state.text("title").isEmpty()
                val status = when (state.text("status")) {
                    "error" -> context.getString(R.string.widget_error)
                    "loading" -> context.getString(R.string.widget_loading)
                    else -> if (empty) context.getString(R.string.widget_empty) else
                        listOf(state.text("artist"), state.text("album")).filter { it.isNotEmpty() }.joinToString(" · ")
                }
                views.setTextViewText(R.id.widget_title, if (empty) context.getString(R.string.app_name_widget) else state.text("title"))
                views.setTextViewText(R.id.widget_subtitle, status)
                views.setContentDescription(R.id.widget_identity, listOf(state.text("title"), status).joinToString(". "))
                val compact = manager.getAppWidgetOptions(id).getInt(AppWidgetManager.OPTION_APPWIDGET_MIN_WIDTH) < 250
                views.setViewVisibility(R.id.widget_art, if (compact) View.GONE else View.VISIBLE)
                if (state.art == null || empty) views.setImageViewResource(R.id.widget_art, R.drawable.ic_widget_music)
                else views.setImageViewBitmap(R.id.widget_art, state.art)
                views.setOnClickPendingIntent(R.id.widget_identity, PendingIntent.getActivity(context, 0, openIntent(context), PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE))
                bind(context, views, R.id.widget_previous, "previous", R.drawable.ic_widget_previous, R.string.widget_previous)
                bind(context, views, R.id.widget_toggle, "toggle", if (state.enabled("playing")) R.drawable.ic_widget_pause else R.drawable.ic_widget_play, if (state.enabled("playing")) R.string.widget_pause else R.string.widget_play)
                bind(context, views, R.id.widget_next, "next", R.drawable.ic_widget_next, R.string.widget_next)
                manager.updateAppWidget(id, views)
            }
        }

        private fun bind(context: Context, views: RemoteViews, view: Int, command: String, icon: Int, label: Int) {
            views.setImageViewResource(view, icon)
            views.setContentDescription(view, context.getString(label))
            val enabled = PlaybackWidgetState.enabled(command)
            views.setFloat(view, "setAlpha", if (enabled) 1f else 0.4f)
            views.setBoolean(view, "setEnabled", enabled)
            // A stale launcher view can still deliver its old PendingIntent;
            // onReceive rechecks process-local state before every command.
            views.setOnClickPendingIntent(view, PendingIntent.getBroadcast(context, view,
                Intent(context, PlaybackWidgetProvider::class.java).setAction(ACTION_PREFIX + command),
                PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE))
        }
    }
}
