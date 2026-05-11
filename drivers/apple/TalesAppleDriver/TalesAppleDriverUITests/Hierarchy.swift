import Foundation
import XCTest

/// HierarchyEncoder turns an XCUIElement snapshot into the JSON shape the
/// Tales mobile provider expects:
///
///   { id, label, text, value, type, enabled, visible, bounds, children }
///
/// Visible is approximated by `(exists && isHittable)` since XCUIElement only
/// exposes those signals on live elements; the provider tolerates this
/// approximation per docs/mobile/ios.md.
enum HierarchyEncoder {
    static func encode(snapshot: XCUIElementSnapshot) -> [String: Any] {
        let frame = snapshot.frame
        return [
            "id": snapshot.identifier,
            "label": snapshot.label,
            "value": stringValue(snapshot.value),
            "type": elementTypeName(snapshot.elementType),
            "enabled": snapshot.isEnabled,
            "visible": snapshot.isSelected || snapshot.hasFocus || frameIsHittable(frame),
            "bounds": [
                "x": Double(frame.origin.x),
                "y": Double(frame.origin.y),
                "width": Double(frame.size.width),
                "height": Double(frame.size.height),
            ],
            "children": snapshot.children.map { encode(snapshot: $0) },
        ]
    }

    private static func frameIsHittable(_ frame: CGRect) -> Bool {
        return frame.width > 0 && frame.height > 0
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

private extension XCUIElementSnapshot {
    var hasFocus: Bool {
        if let attributes = elementType as? XCUIElement.ElementType, attributes == .keyboard {
            return true
        }
        return false
    }
}
