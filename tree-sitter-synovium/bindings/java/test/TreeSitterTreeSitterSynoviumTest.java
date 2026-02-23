import io.github.treesitter.jtreesitter.Language;
import io.github.treesitter.jtreesitter.treesittersynovium.TreeSitterTreeSitterSynovium;
import org.junit.jupiter.api.Test;

import static org.junit.jupiter.api.Assertions.assertDoesNotThrow;

public class TreeSitterTreeSitterSynoviumTest {
    @Test
    public void testCanLoadLanguage() {
        assertDoesNotThrow(() -> new Language(TreeSitterTreeSitterSynovium.language()));
    }
}
