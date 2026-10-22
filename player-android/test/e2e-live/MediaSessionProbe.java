import android.content.Context;
import android.graphics.Bitmap;
import android.media.MediaMetadata;
import android.media.session.MediaController;
import android.media.session.MediaSessionManager;
import android.media.session.PlaybackState;
import java.io.FileOutputStream;
import java.util.List;
import org.json.JSONObject;

/** Read the actual Android OS session, never application/widget test state.
 * Run as shell/root on the disposable emulator with MEDIA_CONTENT_CONTROL.
 */
public final class MediaSessionProbe {
  public static void main(String[] args) throws Exception {
    if (args.length < 1 || !args[0].equals("zone.foo.player_android")) {
      throw new IllegalArgumentException("Exact disposable Player package required");
    }
    if (android.os.Looper.myLooper() == null) android.os.Looper.prepareMainLooper();
    Class<?> activityThread = Class.forName("android.app.ActivityThread");
    Object thread = activityThread.getMethod("systemMain").invoke(null);
    Context context = (Context) activityThread.getMethod("getSystemContext").invoke(thread);
    // app_process bypasses app startup's media framework initialization.
    Class<?> serviceManager = Class.forName("android.media.MediaServiceManager");
    Class<?> initializer = Class.forName("android.media.MediaFrameworkPlatformInitializer");
    initializer.getMethod("setMediaServiceManager", serviceManager)
        .invoke(null, serviceManager.getConstructor().newInstance());
    MediaSessionManager manager = (MediaSessionManager) context.getSystemService(Context.MEDIA_SESSION_SERVICE);
    List<MediaController> controllers = manager.getActiveSessions(null);
    MediaController owned = null;
    for (MediaController controller : controllers) {
      if (controller.getPackageName().equals(args[0])) {
        if (owned != null) throw new IllegalStateException("Multiple Player sessions");
        owned = controller;
      }
    }
    JSONObject result = new JSONObject();
    result.put("package", args[0]);
    result.put("present", owned != null);
    if (owned == null) { System.out.println(result); return; }
    MediaMetadata data = owned.getMetadata();
    PlaybackState state = owned.getPlaybackState();
    result.put("title", data == null ? "" : data.getString(MediaMetadata.METADATA_KEY_TITLE));
    result.put("artist", data == null ? "" : data.getString(MediaMetadata.METADATA_KEY_ARTIST));
    result.put("album", data == null ? "" : data.getString(MediaMetadata.METADATA_KEY_ALBUM));
    result.put("mediaId", data == null ? "" : data.getString(MediaMetadata.METADATA_KEY_MEDIA_ID));
    result.put("state", state == null ? -1 : state.getState());
    result.put("position", state == null ? -1 : state.getPosition());
    Bitmap bitmap = null;
    if (data != null) {
      for (String key : new String[]{MediaMetadata.METADATA_KEY_ALBUM_ART, MediaMetadata.METADATA_KEY_ART, MediaMetadata.METADATA_KEY_DISPLAY_ICON}) {
        bitmap = data.getBitmap(key);
        if (bitmap != null) break;
      }
    }
    result.put("artwork", bitmap != null);
    if (bitmap != null) {
      result.put("width", bitmap.getWidth()); result.put("height", bitmap.getHeight());
      result.put("pixel", bitmap.getPixel(bitmap.getWidth()/2, bitmap.getHeight()/2));
      if (args.length > 1) {
        if (!args[1].startsWith("/data/local/tmp/qk3-")) throw new IllegalArgumentException("Task output path required");
        try (FileOutputStream output = new FileOutputStream(args[1])) {
          bitmap.compress(Bitmap.CompressFormat.PNG, 100, output);
        }
      }
    }
    System.out.println(result);
  }
}
