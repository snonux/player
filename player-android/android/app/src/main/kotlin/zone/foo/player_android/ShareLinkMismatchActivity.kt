package zone.foo.player_android

import android.app.Activity
import android.os.Bundle
import android.view.Gravity
import android.widget.LinearLayout
import android.widget.TextView

/** Explains why a web share URL could not be opened by this Player build. */
class ShareLinkMismatchActivity : Activity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        val padding = (24 * resources.displayMetrics.density).toInt()
        val content = LinearLayout(this).apply {
            orientation = LinearLayout.VERTICAL
            gravity = Gravity.CENTER
            setPadding(padding, padding, padding, padding)
            addView(TextView(this@ShareLinkMismatchActivity).apply {
                text = "Share link is for a different server"
                textSize = 22f
                gravity = Gravity.CENTER
            })
            addView(TextView(this@ShareLinkMismatchActivity).apply {
                text = "This Player build opens shares from a different server address or port. Open the link in a browser or install a build configured for this server."
                textSize = 16f
                gravity = Gravity.CENTER
                setPadding(0, padding, 0, 0)
            })
        }
        setContentView(content)
    }
}
