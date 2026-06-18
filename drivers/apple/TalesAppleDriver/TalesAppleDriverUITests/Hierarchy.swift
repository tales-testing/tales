import Foundation
import XCTest

/// HierarchyEncoder turns an XCUIElement snapshot into the JSON shape the
/// Tales mobile provider expects:
///
///   { id, label, value, type, enabled, visible, bounds, children }
///
/// `visible` is approximated from the snapshot via
/// `isSelected || isKeyboardElement || frameIsHittable(frame)` — XCUIElement
/// only exposes those signals on live elements, and snapshots do not carry
/// real focus / hit-testing data. The provider tolerates this approximation
/// per docs/mobile/ios.md.
enum HierarchyEncoder {
    /// Encodes a snapshot using its dictionary representation rather than the
    /// typed `XCUIElementSnapshot.children` walk. The dictionary is the raw,
    /// fully-serialized accessibility tree; walking `.children` can omit whole
    /// subtrees (notably a SwiftUI NavigationStack's navigation bar and its
    /// `.topBarLeading` toolbar items) intermittently, which surfaced to Tales
    /// as "element not found" on toolbar buttons even though they were present.
    /// This mirrors Maestro's `snapshot().dictionaryRepresentation` approach.
    static func encode(snapshot: XCUIElementSnapshot) -> [String: Any] {
        return encode(dictionary: snapshot.dictionaryRepresentation)
    }

    static func encode(dictionary: [XCUIElement.AttributeName: Any]) -> [String: Any] {
        func attribute(_ name: String) -> Any? {
            dictionary[XCUIElement.AttributeName(rawValue: name)]
        }

        let elementTypeRaw = attribute("elementType") as? Int ?? 0
        let elementType = XCUIElement.ElementType(rawValue: UInt(elementTypeRaw)) ?? .other
        let (x, y, width, height) = frameComponents(attribute("frame"))
        let selected = attribute("selected") as? Bool ?? false
        let childrenDicts = attribute("children") as? [[XCUIElement.AttributeName: Any]] ?? []

        return [
            "id": attribute("identifier") as? String ?? "",
            "label": attribute("label") as? String ?? "",
            "value": stringValue(attribute("value")),
            "type": elementTypeName(elementType),
            "enabled": attribute("enabled") as? Bool ?? false,
            "visible": selected || elementType == .keyboard || (width > 0 && height > 0),
            "bounds": [
                "x": x,
                "y": y,
                "width": width,
                "height": height,
            ],
            "children": childrenDicts.map { encode(dictionary: $0) },
        ]
    }

    /// Extracts x/y/width/height from a snapshot dictionary's "frame" value.
    /// XCTest exposes it as an X/Y/Width/Height dictionary on recent runtimes,
    /// but tolerate a CGRect / NSValue form too so a future change does not
    /// silently zero out every element's bounds (which would break taps).
    private static func frameComponents(_ value: Any?) -> (Double, Double, Double, Double) {
        if let frame = value as? [String: Double] {
            return (frame["X"] ?? 0, frame["Y"] ?? 0, frame["Width"] ?? 0, frame["Height"] ?? 0)
        }

        if let rect = value as? CGRect {
            return (Double(rect.origin.x), Double(rect.origin.y), Double(rect.size.width), Double(rect.size.height))
        }

        if let nsValue = value as? NSValue {
            let rect = nsValue.cgRectValue

            return (Double(rect.origin.x), Double(rect.origin.y), Double(rect.size.width), Double(rect.size.height))
        }

        return (0, 0, 0, 0)
    }

    private static func stringValue(_ value: Any?) -> String {
        guard let value = value else { return "" }
        if let s = value as? String { return s }
        return String(describing: value)
    }

    private static func elementTypeName(_ type: XCUIElement.ElementType) -> String {
        switch type {
        case .application: return "application"
        case .window: return "window"
        case .button: return "button"
        case .image: return "image"
        case .staticText: return "static_text"
        case .textField: return "text_field"
        case .secureTextField: return "secure_text_field"
        case .textView: return "text_view"
        case .table: return "table"
        case .cell: return "cell"
        case .collectionView: return "collection_view"
        case .navigationBar: return "navigation_bar"
        case .tabBar: return "tab_bar"
        case .tabGroup: return "tab_group"
        case .toolbar: return "toolbar"
        case .switch: return "switch"
        case .alert: return "alert"
        case .sheet: return "sheet"
        case .other: return "other"
        case .activityIndicator: return "activity_indicator"
        case .scrollView: return "scroll_view"
        default: return "unknown"
        }
    }
}
