import 'package:integration_test/integration_test.dart';
import '../test/screens/podcast_multiselect_test.dart' as selection;

// Actual native controls at constrained portrait/landscape surfaces and enlarged
// MediaQuery text; this does not physically rotate the emulator display.
void main() {
  IntegrationTestWidgetsFlutterBinding.ensureInitialized();
  selection.podcastSelectionLayoutTests();
}
