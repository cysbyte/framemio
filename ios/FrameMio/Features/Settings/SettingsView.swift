import SwiftUI

struct SettingsView: View {
    @ObservedObject private var viewModel: SettingsViewModel

    init(viewModel: SettingsViewModel) {
        _viewModel = ObservedObject(wrappedValue: viewModel)
    }

    var body: some View {
        List {
            Section {
                VStack(alignment: .leading, spacing: FrameMioSpacing.sm) {
                    Text("Your memories stay personal.")
                        .font(FrameMioTypography.editorialTitle)
                        .foregroundStyle(FrameMioColors.ink)
                    Text("FrameMio uses your iPhone library to understand moments, then sends only the information needed to create a story. No backend secrets live in the app.")
                        .font(FrameMioTypography.body)
                        .foregroundStyle(FrameMioColors.secondaryInk)
                }
                .padding(.vertical, FrameMioSpacing.sm)
            }

            Section("Photo Library") {
                HStack {
                    Label("Access", systemImage: "photo.on.rectangle")
                    Spacer()
                    Text(statusTitle)
                        .foregroundStyle(FrameMioColors.secondaryInk)
                }
                .accessibilityElement(children: .combine)

                if viewModel.authorization != .authorized && viewModel.authorization != .limited {
                    Button("Allow Photo Access") { viewModel.requestPhotoAccess() }
                }
            }

            Section("About") {
                HStack {
                    Text("Version")
                    Spacer()
                    Text("1.0")
                        .foregroundStyle(FrameMioColors.secondaryInk)
                }
            }
        }
        .scrollContentBackground(.hidden)
        .background(FrameMioColors.canvas)
        .navigationTitle("Settings")
        .task { viewModel.refresh() }
        .alert("Photo access", isPresented: errorBinding) {
            Button("Dismiss", role: .cancel) { viewModel.errorMessage = nil }
        } message: {
            Text(viewModel.errorMessage ?? "")
        }
    }

    private var statusTitle: String {
        switch viewModel.authorization {
        case .authorized: "Allowed"
        case .limited: "Limited"
        case .notDetermined: "Not set"
        case .restricted: "Restricted"
        case .denied: "Not allowed"
        }
    }

    private var errorBinding: Binding<Bool> {
        Binding(
            get: { viewModel.errorMessage != nil },
            set: { if !$0 { viewModel.errorMessage = nil } }
        )
    }
}
