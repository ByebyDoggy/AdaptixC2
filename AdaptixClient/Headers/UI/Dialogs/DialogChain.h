#ifndef ADAPTIXCLIENT_DIALOGCHAIN_H
#define ADAPTIXCLIENT_DIALOGCHAIN_H

#include <main.h>

class AuthProfile;

class DialogChain : public QWidget
{
Q_OBJECT

    AuthProfile* profile = nullptr;

    QLineEdit*   domainEdit   = nullptr;
    QLineEdit*   rpcEdit      = nullptr;
    QLineEdit*   contractEdit = nullptr;
    QSpinBox*    chainIdSpin  = nullptr;
    QSpinBox*    pollSpin     = nullptr;
    QCheckBox*   enabledCheck = nullptr;

    QLabel*      statusLabel  = nullptr;

    QPushButton* saveButton     = nullptr;
    QPushButton* refreshButton  = nullptr;
    QPushButton* calldataButton = nullptr;
    QPushButton* closeButton    = nullptr;

    void createUI();
    void setBusy(bool busy);
    void setStatus(const QString &text, bool error);
    void applyConfig(const QJsonObject &config);
    QJsonObject currentConfig() const;
    bool validateInputs(QString &error) const;

public:
    explicit DialogChain(AuthProfile* authProfile, QWidget* parent = nullptr);
    ~DialogChain() override;

public Q_SLOTS:
    void onSave();
    void onRefreshStatus();
    void onCopyCalldata();
};

#endif
