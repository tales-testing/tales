import Foundation
import Network

/// TalesHTTPServer is a minimal HTTP/1.1 server suited for the Tales driver
/// contract. It supports a single concurrent request, GET / POST,
/// Content-Length-delimited bodies, JSON request payloads, and JSON or PNG
/// responses. The implementation intentionally targets the smallest viable
/// surface: this is not a general-purpose HTTP server, it just serves the
/// well-known Tales driver routes.
final class TalesHTTPServer {
    private let host: String
    private let port: Int
    private let queue = DispatchQueue(label: "tales.driver.http")
    private var listener: NWListener?

    init(host: String, port: Int) {
        self.host = host
        self.port = port
    }

    func start() throws {
        let parameters = NWParameters.tcp
        parameters.allowLocalEndpointReuse = true
        parameters.requiredLocalEndpoint = NWEndpoint.hostPort(
            host: NWEndpoint.Host(self.host),
            port: NWEndpoint.Port(integerLiteral: UInt16(self.port))
        )

        let listener = try NWListener(using: parameters, on: NWEndpoint.Port(integerLiteral: UInt16(self.port)))
        listener.newConnectionHandler = { [weak self] connection in
            self?.handle(connection: connection)
        }
        listener.stateUpdateHandler = { state in
            switch state {
            case .failed(let error):
                NSLog("[tales-driver] listener failed: \(error)")
            default:
                break
            }
        }
        listener.start(queue: queue)
        self.listener = listener
    }

    func stop() {
        listener?.cancel()
        listener = nil
    }

    private func handle(connection: NWConnection) {
        connection.start(queue: queue)
        receive(connection: connection, buffer: Data())
    }

    private func receive(connection: NWConnection, buffer: Data) {
        connection.receive(minimumIncompleteLength: 1, maximumLength: 64 * 1024) { [weak self] data, _, isComplete, error in
            guard let self else { return }

            if let error {
                NSLog("[tales-driver] receive error: \(error)")
                connection.cancel()
                return
            }

            var current = buffer
            if let data = data, !data.isEmpty {
                current.append(data)
            }

            if let (request, _) = HTTPRequest.parse(current) {
                let response = TalesRouter.shared.dispatch(request: request)
                connection.send(content: response.encoded(), completion: .contentProcessed { _ in
                    connection.cancel()
                })
                return
            }

            if isComplete {
                connection.cancel()
                return
            }

            self.receive(connection: connection, buffer: current)
        }
    }
}

struct HTTPRequest {
    let method: String
    let path: String
    let query: [String: String]
    let headers: [String: String]
    let body: Data

    static func parse(_ raw: Data) -> (HTTPRequest, Int)? {
        guard let sepRange = raw.range(of: Data("\r\n\r\n".utf8)) else {
            return nil
        }

        let headerData = raw.subdata(in: 0..<sepRange.lowerBound)
        let bodyStart = sepRange.upperBound

        guard let headerString = String(data: headerData, encoding: .utf8) else {
            return nil
        }

        let lines = headerString.components(separatedBy: "\r\n")
        guard let requestLine = lines.first else { return nil }

        let parts = requestLine.components(separatedBy: " ")
        guard parts.count >= 2 else { return nil }

        let method = parts[0]
        let rawPath = parts[1]

        let (path, query) = parsePathAndQuery(rawPath)

        var headers: [String: String] = [:]
        for line in lines.dropFirst() where !line.isEmpty {
            if let colon = line.firstIndex(of: ":") {
                let key = line[line.startIndex..<colon].trimmingCharacters(in: .whitespaces).lowercased()
                let value = line[line.index(after: colon)...].trimmingCharacters(in: .whitespaces)
                headers[key] = value
            }
        }

        let contentLength = Int(headers["content-length"] ?? "0") ?? 0
        if raw.count < bodyStart + contentLength {
            return nil
        }

        let body = raw.subdata(in: bodyStart..<bodyStart + contentLength)
        let request = HTTPRequest(method: method, path: path, query: query, headers: headers, body: body)
        return (request, bodyStart + contentLength)
    }

    private static func parsePathAndQuery(_ raw: String) -> (String, [String: String]) {
        if let q = raw.firstIndex(of: "?") {
            let path = String(raw[raw.startIndex..<q])
            let queryString = String(raw[raw.index(after: q)...])
            var dict: [String: String] = [:]
            for item in queryString.components(separatedBy: "&") {
                let pair = item.components(separatedBy: "=")
                if pair.count == 2 {
                    dict[pair[0]] = pair[1].removingPercentEncoding ?? pair[1]
                }
            }
            return (path, dict)
        }
        return (raw, [:])
    }
}

struct HTTPResponse {
    let status: Int
    let reason: String
    let contentType: String
    let body: Data

    static func json(_ object: Any, status: Int = 200) -> HTTPResponse {
        let data = (try? JSONSerialization.data(withJSONObject: object, options: [])) ?? Data("{}".utf8)
        return HTTPResponse(status: status, reason: reasonPhrase(status), contentType: "application/json", body: data)
    }

    static func error(_ message: String, status: Int = 500) -> HTTPResponse {
        let body = Data("{\"error\":\"\(message.replacingOccurrences(of: "\"", with: "'"))\"}".utf8)
        return HTTPResponse(status: status, reason: reasonPhrase(status), contentType: "application/json", body: body)
    }

    static func png(_ data: Data) -> HTTPResponse {
        HTTPResponse(status: 200, reason: "OK", contentType: "image/png", body: data)
    }

    private static func reasonPhrase(_ status: Int) -> String {
        switch status {
        case 200: return "OK"
        case 400: return "Bad Request"
        case 404: return "Not Found"
        case 500: return "Internal Server Error"
        case 503: return "Service Unavailable"
        default: return "Status"
        }
    }

    func encoded() -> Data {
        var head = "HTTP/1.1 \(status) \(reason)\r\n"
        head += "Content-Type: \(contentType)\r\n"
        head += "Content-Length: \(body.count)\r\n"
        head += "Connection: close\r\n\r\n"

        var out = Data(head.utf8)
        out.append(body)
        return out
    }
}
