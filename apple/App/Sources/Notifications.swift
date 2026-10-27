import SwiftUI
import UserNotifications
import CorrespondentKit

/// Notifications through Apple's push service: the device's token goes to
/// the reader's server, which sends when a feed with the bell has news.
/// Tapping one opens its article.
@MainActor
final class Notifications: NSObject, UNUserNotificationCenterDelegate {
    static let shared = Notifications()
    weak var app: AppModel?

    /// The last token iOS or macOS gave, kept to unregister it later.
    var token: String? {
        get { UserDefaults.standard.string(forKey: "apnsToken") }
        set { UserDefaults.standard.set(newValue, forKey: "apnsToken") }
    }

    /// Development builds register in the APNs sandbox, the rest in production.
    static var environment: String {
        #if DEBUG
        "sandbox"
        #else
        "production"
        #endif
    }

    /// Asks permission and registers for remote notifications.
    func enable() async -> Bool {
        let center = UNUserNotificationCenter.current()
        center.delegate = self
        guard (try? await center.requestAuthorization(options: [.alert, .sound, .badge])) == true else { return false }
        #if os(iOS)
        UIApplication.shared.registerForRemoteNotifications()
        #else
        NSApplication.shared.registerForRemoteNotifications()
        #endif
        return true
    }

    func disable() async {
        if let token, let client = app?.client { try? await client.unregisterDevice(token) }
        #if os(iOS)
        UIApplication.shared.unregisterForRemoteNotifications()
        #else
        NSApplication.shared.unregisterForRemoteNotifications()
        #endif
    }

    func registered(_ deviceToken: Data) {
        let hex = deviceToken.map { String(format: "%02x", $0) }.joined()
        token = hex
        Task {
            do {
                try await app?.client?.registerDevice(hex, environment: Self.environment)
            } catch {
                app?.show("Couldn't turn on notifications. \(error.localizedDescription)")
            }
        }
    }

    // The completion-handler forms, answered on the main thread: with the
    // async forms iOS finishes the call on a background thread, and UIKit
    // ends the app (a tap on a notification crashed it in TestFlight).

    // Shown even with the app open, as a banner.
    nonisolated func userNotificationCenter(_ center: UNUserNotificationCenter, willPresent notification: UNNotification,
                                            withCompletionHandler completionHandler: @escaping (UNNotificationPresentationOptions) -> Void) {
        nonisolated(unsafe) let done = completionHandler
        DispatchQueue.main.async { done([.banner, .sound]) }
    }

    // A tap opens the article (or the unread list for several).
    nonisolated func userNotificationCenter(_ center: UNUserNotificationCenter, didReceive response: UNNotificationResponse,
                                            withCompletionHandler completionHandler: @escaping () -> Void) {
        let info = response.notification.request.content.userInfo
        let entry = (info["entry"] as? String) ?? (info["entry"] as? NSNumber)?.stringValue
        nonisolated(unsafe) let done = completionHandler
        DispatchQueue.main.async {
            MainActor.assumeIsolated {
                let url = entry.flatMap { URL(string: "correspondent://entry/\($0)") } ?? URL(string: "correspondent://unread")!
                IntentRouter.shared.pending = url
            }
            done()
        }
    }
}

#if os(iOS)
final class AppDelegate: NSObject, UIApplicationDelegate {
    func application(_ application: UIApplication, didFinishLaunchingWithOptions launchOptions: [UIApplication.LaunchOptionsKey: Any]? = nil) -> Bool {
        UNUserNotificationCenter.current().delegate = Notifications.shared
        return true
    }
    func application(_ application: UIApplication, didRegisterForRemoteNotificationsWithDeviceToken deviceToken: Data) {
        Notifications.shared.registered(deviceToken)
    }
    func application(_ application: UIApplication, didFailToRegisterForRemoteNotificationsWithError error: Error) {
        Notifications.shared.app?.show("Notifications aren't available on this device. \(error.localizedDescription)")
    }
}
#else
final class AppDelegate: NSObject, NSApplicationDelegate {
    func applicationDidFinishLaunching(_ notification: Notification) {
        UNUserNotificationCenter.current().delegate = Notifications.shared
    }
    func application(_ application: NSApplication, didRegisterForRemoteNotificationsWithDeviceToken deviceToken: Data) {
        Notifications.shared.registered(deviceToken)
    }
    func application(_ application: NSApplication, didFailToRegisterForRemoteNotificationsWithError error: Error) {
        Notifications.shared.app?.show("Notifications aren't available on this Mac. \(error.localizedDescription)")
    }
}
#endif
