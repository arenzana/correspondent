import Foundation
import Security

/// The signed-in account: the server, the username, and the token, which
/// lives in the Keychain (this device only, after first unlock).
public struct Account: Codable, Hashable, Sendable {
    public var server: URL
    public var username: String

    public init(server: URL, username: String) {
        self.server = server
        self.username = username
    }

    /// Identifies the account's offline copy.
    public var key: String { "\(username)@\(server.host() ?? "server")" }

    private static let defaultsKey = "account"
    private static let service = "org.arenzana.correspondent.token"

    /// The saved account and its token, if signed in.
    public static func load() -> (Account, String)? {
        guard let data = UserDefaults.standard.data(forKey: defaultsKey),
              let account = try? JSONDecoder().decode(Account.self, from: data),
              let token = Keychain.read(service: service, account: account.key) else { return nil }
        return (account, token)
    }

    public func save(token: String) throws {
        try Keychain.write(token, service: Self.service, account: key)
        UserDefaults.standard.set(try JSONEncoder().encode(self), forKey: Self.defaultsKey)
    }

    public func forget() {
        Keychain.delete(service: Self.service, account: key)
        UserDefaults.standard.removeObject(forKey: Self.defaultsKey)
    }
}

enum Keychain {
    static func base(service: String, account: String) -> [String: Any] {
        [kSecClass as String: kSecClassGenericPassword,
         kSecAttrService as String: service,
         kSecAttrAccount as String: account]
    }

    static func read(service: String, account: String) -> String? {
        var q = base(service: service, account: account)
        q[kSecReturnData as String] = true
        q[kSecMatchLimit as String] = kSecMatchLimitOne
        var out: AnyObject?
        guard SecItemCopyMatching(q as CFDictionary, &out) == errSecSuccess, let data = out as? Data else { return nil }
        return String(data: data, encoding: .utf8)
    }

    static func write(_ value: String, service: String, account: String) throws {
        delete(service: service, account: account)
        var q = base(service: service, account: account)
        q[kSecValueData as String] = Data(value.utf8)
        q[kSecAttrAccessible as String] = kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly
        let status = SecItemAdd(q as CFDictionary, nil)
        guard status == errSecSuccess else {
            throw APIError(status: Int(status), message: "Couldn't keep the sign-in in the Keychain (\(status)).")
        }
    }

    static func delete(service: String, account: String) {
        SecItemDelete(base(service: service, account: account) as CFDictionary)
    }
}
