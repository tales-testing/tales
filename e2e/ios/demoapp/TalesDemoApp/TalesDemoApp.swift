import SwiftUI

@main
struct TalesDemoApp: App {
    var body: some Scene {
        WindowGroup {
            DemoFlowView()
        }
    }
}

private enum DemoScreen {
    case welcome
    case register
    case verify
    case home
}

struct DemoFlowView: View {
    @State private var screen: DemoScreen = .welcome
    @State private var email = ""
    @State private var password = ""
    @State private var verificationCode = ""
    @State private var registerError = ""
    @State private var verifyError = ""

    var body: some View {
        NavigationStack {
            content
                .padding(24)
                .frame(maxWidth: .infinity, maxHeight: .infinity)
                .background(Color(.systemBackground))
        }
    }

    @ViewBuilder
    private var content: some View {
        switch screen {
        case .welcome:
            welcome
        case .register:
            register
        case .verify:
            verify
        case .home:
            home
        }
    }

    private var welcome: some View {
        VStack(spacing: 24) {
            Text("Tales Demo")
                .font(.largeTitle.bold())
                .accessibilityIdentifier("welcome.title")

            Button("Register") {
                screen = .register
            }
            .buttonStyle(.borderedProminent)
            .accessibilityIdentifier("welcome.register")
        }
    }

    private var register: some View {
        VStack(spacing: 16) {
            Text("Register screen")
                .font(.caption2)
                .foregroundStyle(.secondary)
                .accessibilityIdentifier("register.screen")

            Text("Create account")
                .font(.title.bold())

            TextField("Email", text: $email)
                .textInputAutocapitalization(.never)
                .keyboardType(.emailAddress)
                .autocorrectionDisabled()
                .textFieldStyle(.roundedBorder)
                .accessibilityIdentifier("register.email")

            SecureField("Password", text: $password)
                .textFieldStyle(.roundedBorder)
                .accessibilityIdentifier("register.password")

            if !registerError.isEmpty {
                Text(registerError)
                    .foregroundStyle(.red)
                    .accessibilityIdentifier("register.error")
            }

            Button("Submit") {
                submitRegister()
            }
            .buttonStyle(.borderedProminent)
            .accessibilityIdentifier("register.submit")
        }
    }

    private var verify: some View {
        VStack(spacing: 16) {
            Text("Verification screen")
                .font(.caption2)
                .foregroundStyle(.secondary)
                .accessibilityIdentifier("verify.screen")

            Text("Verify account")
                .font(.title.bold())

            TextField("Verification code", text: $verificationCode)
                .textInputAutocapitalization(.characters)
                .autocorrectionDisabled()
                .textFieldStyle(.roundedBorder)
                .accessibilityIdentifier("verify.code")

            if !verifyError.isEmpty {
                Text(verifyError)
                    .foregroundStyle(.red)
                    .accessibilityIdentifier("verify.error")
            }

            Button("Verify") {
                submitVerification()
            }
            .buttonStyle(.borderedProminent)
            .accessibilityIdentifier("verify.submit")
        }
    }

    private var home: some View {
        VStack(spacing: 16) {
            Text("Home screen")
                .font(.caption2)
                .foregroundStyle(.secondary)
                .accessibilityIdentifier("home.screen")

            Text("Welcome")
                .font(.largeTitle.bold())
                .accessibilityIdentifier("home.title")

            Text(email)
                .accessibilityIdentifier("home.email")
        }
    }

    private func submitRegister() {
        if email.isEmpty || !email.contains("@") {
            registerError = "Enter a valid email"
            return
        }

        if password.isEmpty {
            registerError = "Enter a password"
            return
        }

        registerError = ""
        screen = .verify
    }

    private func submitVerification() {
        if verificationCode.uppercased() != "A1B2C3" {
            verifyError = "Invalid verification code"
            return
        }

        verifyError = ""
        screen = .home
    }
}
