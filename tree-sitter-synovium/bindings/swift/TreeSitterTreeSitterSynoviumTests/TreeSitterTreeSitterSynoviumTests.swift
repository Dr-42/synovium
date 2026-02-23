import XCTest
import SwiftTreeSitter
import TreeSitterTreeSitterSynovium

final class TreeSitterTreeSitterSynoviumTests: XCTestCase {
    func testCanLoadGrammar() throws {
        let parser = Parser()
        let language = Language(language: tree_sitter_tree_sitter_synovium())
        XCTAssertNoThrow(try parser.setLanguage(language),
                         "Error loading TreeSitter Synovium grammar")
    }
}
