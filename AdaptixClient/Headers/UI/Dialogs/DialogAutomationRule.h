#ifndef ADAPTIXCLIENT_DIALOGAUTOMATIONRULE_H
#define ADAPTIXCLIENT_DIALOGAUTOMATIONRULE_H

#include <main.h>

class AuthProfile;

// DialogAutomationRule edits the configuration of a single auto_exec rule.
// The rule itself is defined in the teamserver profile; this dialog only
// changes the runtime overrides (name, trigger, order, commands, template and
// channel references).
class DialogAutomationRule : public QDialog
{
Q_OBJECT

    AuthProfile* profile = nullptr;
    QJsonObject  rule;

    QLineEdit*   nameEdit     = nullptr;
    QLineEdit*   descEdit     = nullptr;
    QComboBox*   triggerCombo = nullptr;
    QSpinBox*    orderSpin    = nullptr;
    QSpinBox*    delaySpin    = nullptr;
    QCheckBox*   enabledCheck = nullptr;

    QTextEdit*   commandsEdit        = nullptr;
    QComboBox*   artexCombo          = nullptr;
    QComboBox*   notifyChannelCombo  = nullptr;
    QComboBox*   notifyTemplateCombo = nullptr;
    QLabel*      scriptLabel         = nullptr;
    QLabel*      persistLabel        = nullptr;

    QLabel*      statusLabel  = nullptr;
    QPushButton* saveButton   = nullptr;
    QPushButton* cancelButton = nullptr;

    void createUI(const QJsonArray &artexTemplates, const QJsonArray &notifyTemplates, const QJsonArray &channels);
    QJsonObject buildPatch() const;

public:
    DialogAutomationRule(AuthProfile* authProfile, const QJsonObject &ruleData,
                         const QJsonArray &artexTemplates, const QJsonArray &notifyTemplates,
                         const QJsonArray &channels, QWidget* parent = nullptr);
    ~DialogAutomationRule() override;

public Q_SLOTS:
    void onSave();
};

#endif
