import 'package:integration_test/integration_test.dart';

import '../test/widgets/keyboard_access_test.dart' as keyboard;

// Run the same native menu, editable-field, slider and scrolling journeys
// against Android's actual semantics/rendering pipeline on a device.
void main() {
  IntegrationTestWidgetsFlutterBinding.ensureInitialized();
  keyboard.main();
}
