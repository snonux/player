import 'package:integration_test/integration_test.dart';
import '../test/screens/podcast_episode_layout_test.dart' as layout;

// Actual native screen/controls, with deterministic cached repository data.
void main() {
  IntegrationTestWidgetsFlutterBinding.ensureInitialized();
  layout.episodeLayoutTests();
}
