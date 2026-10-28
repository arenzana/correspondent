import CoreLocation
import CorrespondentKit

/// One rough position for the Sun theme, as the web's geolocation call:
/// asked for once, at reduced accuracy, and rounded to about 11 km before
/// it's kept. Nil when refused, unavailable or slow; the caller then
/// estimates from the time zone.
@MainActor
final class Locator: NSObject, @preconcurrency CLLocationManagerDelegate {
    private let manager = CLLocationManager()
    private var waiting: CheckedContinuation<CLLocation?, Never>?

    static func place() async -> Prefs.Place? {
        let locator = Locator()
        guard let found = await locator.locate() else { return nil }
        return Prefs.Place(lat: Sun.coarse(found.coordinate.latitude), lon: Sun.coarse(found.coordinate.longitude), estimated: false)
    }

    private func locate() async -> CLLocation? {
        manager.delegate = self
        manager.desiredAccuracy = kCLLocationAccuracyReduced
        return await withCheckedContinuation { c in
            waiting = c
            Task { try? await Task.sleep(for: .seconds(10)); finish(nil) }
            proceed()
        }
    }

    private func proceed() {
        switch manager.authorizationStatus {
        case .notDetermined: manager.requestWhenInUseAuthorization() // answered in the delegate
        case .denied, .restricted: finish(nil)
        default: manager.requestLocation()
        }
    }

    private func finish(_ location: CLLocation?) {
        waiting?.resume(returning: location)
        waiting = nil
    }

    func locationManagerDidChangeAuthorization(_ manager: CLLocationManager) {
        if waiting != nil, manager.authorizationStatus != .notDetermined { proceed() }
    }

    func locationManager(_ manager: CLLocationManager, didUpdateLocations locations: [CLLocation]) { finish(locations.last) }

    func locationManager(_ manager: CLLocationManager, didFailWithError error: Error) { finish(nil) }
}
