import android.app.UiAutomation;
import android.graphics.Rect;
import android.view.accessibility.AccessibilityNodeInfo;
import org.json.JSONArray;
import org.json.JSONObject;
import java.lang.reflect.Method;

public final class MusicAccessibilityProbe {
  static void visit(AccessibilityNodeInfo node, JSONArray result) throws Exception {
    if (node == null) return;
    JSONObject value = new JSONObject();
    Rect bounds = new Rect(); node.getBoundsInScreen(bounds);
    value.put("class", String.valueOf(node.getClassName()));
    value.put("text", String.valueOf(node.getText()));
    value.put("description", String.valueOf(node.getContentDescription()));
    value.put("hint", String.valueOf(node.getHintText()));
    value.put("viewId", String.valueOf(node.getViewIdResourceName()));
    value.put("editable", node.isEditable());
    value.put("clickable", node.isClickable());
    value.put("enabled", node.isEnabled());
    value.put("visible", node.isVisibleToUser());
    value.put("bounds", new JSONArray(new int[]{bounds.left,bounds.top,bounds.right,bounds.bottom}));
    result.put(value);
    for (int i=0;i<node.getChildCount();i++) visit(node.getChild(i), result);
    node.recycle();
  }
  public static void main(String[] args) throws Exception {
    Class<?> type = Class.forName("com.android.uiautomator.core.UiAutomationShellWrapper");
    Object shell = type.getDeclaredConstructor().newInstance();
    Method connect = type.getDeclaredMethod("connect"); connect.setAccessible(true);
    Method disconnect = type.getDeclaredMethod("disconnect"); disconnect.setAccessible(true);
    Method automation = type.getDeclaredMethod("getUiAutomation"); automation.setAccessible(true);
    try {
      connect.invoke(shell);
      UiAutomation ui = (UiAutomation) automation.invoke(shell);
      JSONArray nodes = new JSONArray();
      int polls = 0;
      long deadline = android.os.SystemClock.uptimeMillis() + 7000;
      boolean editable = false;
      do {
        nodes = new JSONArray();
        visit(ui.getRootInActiveWindow(), nodes);
        polls++;
        for (int i=0;i<nodes.length();i++) {
          if (nodes.getJSONObject(i).getBoolean("editable")) editable = true;
        }
        if (editable) break;
        android.os.SystemClock.sleep(100);
      } while (android.os.SystemClock.uptimeMillis() < deadline);
      System.out.println(new JSONObject().put("nodes",nodes).put("freshRootPolls",polls).toString());
    } finally { disconnect.invoke(shell); }
    System.exit(0);
  }
}
