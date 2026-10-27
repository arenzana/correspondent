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

    /// Shared with the app's extensions (share sheet, widgets): the App
    /// Group's defaults hold the account, the shared Keychain group the token.
    public static let appGroup = "group.org.arenzana.correspondent"
    /// The Keychain group, set by the app at launch from its signing prefix.
    nonisolated(unsafe) public static var keychainGroup: String?

    private static var shared: UserDefaults { UserDefaults(suiteName: appGroup) ?? .standard }

    /// The saved account and its token, if signed in.
    public static func load() -> (Account, String)? {
        // Earlier builds kept it in the app's own defaults and Keychain: carry it over.
        for defaults in [shared, UserDefaults.standard] {
            guard let data = defaults.data(forKey: defaultsKey),
                  let account = try? JSONDecoder().decode(Account.self, from: data) else { continue }
            if let token = Keychain.read(service: service, account: account.key, group: keychainGroup) { return (account, token) }
            if let token = Keychain.read(service: service, account: account.key, group: nil) {
                try? account.save(token: token)
                return (account, token)
            }
        }
        return nil
    }

    public func save(token: String) throws {
        try Keychain.write(token, service: Self.service, account: key, group: Self.keychainGroup)
        Self.shared.set(try JSONEncoder().encode(self), forKey: Self.defaultsKey)
    }

    public func forget() {
        Keychain.delete(service: Self.service, account: key, group: Self.keychainGroup)
        Keychain.delete(service: Self.service, account: key, group: nil)
        Self.shared.removeObject(forKey: Self.defaultsKey)
        UserDefaults.standard.removeObject(forKey: Self.defaultsKey)
    }
}

enum Keychain {
    static func base(service: String, account: String, group: String? = nil) -> [String: Any] {
        var q: [String: Any] = [kSecClass as String: kSecClassGenericPassword,
                                kSecAttrService as String: service,
                                kSecAttrAccount as String: account]
        if let group { q[kSecAttrAccessGroup as String] = group }
        return q
    }

    static func read(service: String, account: String, group: String? = nil) -> String? {
        var q = base(service: service, account: account, group: group)
        q[kSecReturnData as String] = true
        q[kSecMatchLimit as String] = kSecMatchLimitOne
        var out: AnyObject?
        guard SecItemCopyMatching(q as CFDictionary, &out) == errSecSuccess, let data = out as? Data else { return nil }
        return String(data: data, encoding: .utf8)
    }

    static func write(_ value: String, service: String, account: String, group: String? = nil) throws {
        delete(service: service, account: account, group: group)
        var q = base(service: service, account: account, group: group)
        q[kSecValueData as String] = Data(value.utf8)
        q[kSecAttrAccessible as String] = kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly
        let status = SecItemAdd(q as CFDictionary, nil)
        guard status == errSecSuccess else {
            throw APIError(status: Int(status), message: "Couldn't keep the sign-in in the Keychain (\(status)).")
        }
    }

    static func delete(service: String, account: String, group: String? = nil) {
        SecItemDelete(base(service: service, account: account, group: group) as CFDictionary)
    }
}
